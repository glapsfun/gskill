package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// migrateRunOptions parameterizes one mutating command's auto-migration pass
// (spec 022 FR-013).
type migrateRunOptions struct {
	// frozen: never rewrite the lockfile — convert links and local state
	// only; lock-field cleanup waits for the next non-frozen mutation.
	frozen bool
	// skip names skills the current run removes: they are removed directly,
	// never converted first (no content fetch for content about to go away).
	skip map[string]bool
	// dryRun runs write nothing at all — migration included.
	dryRun bool
	// offline forbids network fetches during migration, exactly as it does
	// for the command that triggered it: a legacy project must not silently
	// reach the network under --offline.
	offline bool
}

// loadLockMigrated loads the lock and runs the auto-migration prelude —
// the shared opening move of every mutating flow.
func (a *App) loadLockMigrated(ctx context.Context, p *project, opts migrateRunOptions) (*skillslock.State, error) {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return nil, err
	}
	if !opts.dryRun {
		if err := a.autoMigrate(ctx, p, lf, opts); err != nil {
			return nil, err
		}
	}
	return lf, nil
}

// autoMigrate converts a legacy (pre-022) project to the repo-owned layout as
// a transparent prelude to any mutating command (spec 022 FR-013): legacy
// store symlinks become committed copies, agent links become relative, and
// rewritten lock entries drop their store fields. It runs under the caller's
// project lock, never touches the old store's bytes (FR-014), and emits
// exactly one notice line when anything migrated. Reproduction preference per
// skill: a hash-valid old store object (offline), the commit-keyed clone
// cache, then a network fetch by the recorded source and commit.
func (a *App) autoMigrate(ctx context.Context, p *project, lf *skillslock.State, opts migrateRunOptions) error {
	// Un-ignore the committed layer first: pre-022 gskill wrote a ".agents/"
	// ignore line, and Init (the only other caller of the gitignore fix-up)
	// never runs for an already-initialized project — leaving it in place
	// would keep the migrated content out of version control (FR-010). Pure
	// bookkeeping: a failure warns rather than failing the command.
	if _, err := unignoreAgentsLayer(p.root); err != nil {
		a.log.Warn("un-ignore .agents/ in .gitignore", "error", err)
	}

	if err := a.ensureManifest(p, lf, opts); err != nil {
		return err
	}

	var migrated []string
	for _, name := range sortedKeys(lf.Skills) {
		if opts.skip[name] {
			continue
		}
		locked := lf.Skills[name]
		if !a.isLegacySkill(p, name, locked) {
			continue
		}
		if err := a.migrateSkill(ctx, p, lf, name, locked, opts); err != nil {
			return fmt.Errorf("migrate skill %q to repo-owned storage: %w", name, err)
		}
		migrated = append(migrated, name)
	}
	if len(migrated) == 0 {
		return nil
	}
	if !opts.frozen {
		if err := saveLock(p.lockPath, lf); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(a.notice, "migrated %d skill(s) to repo-owned storage; the old store at %s was left untouched\n",
		len(migrated), a.legacyNoticePath(p))
	return nil
}

// isLegacySkill classifies a locked skill as pre-022: its active entry or an
// agent link is a symlink into a known legacy store root, or its lock entry
// still carries the retired store-scope field (data-model §9). Entries
// without gskill content identity (external tools') are never touched, and
// agent-global installs live outside the repo entirely.
func (a *App) isLegacySkill(p *project, name string, locked skillslock.Record) bool {
	hash := locked.Resolved.ContentHash
	if hash == "" || locked.Installation.Scope == "global" {
		return false
	}
	if locked.Installation.Scope != "" {
		return true
	}
	legacyRoots := p.legacyStoreRoots()
	if h, err := active.HealthOf(p.root, name, hash, legacyRoots...); err == nil && h == active.HealthLegacy {
		return true
	}
	for _, rel := range locked.Installation.Targets {
		target := resolveTarget(p.root, rel)
		if managed, err := managedBySymlink(target, legacyRoots...); err == nil && managed {
			return true
		}
	}
	return false
}

// migrateSkill converts one skill: seed the committed copy from a hash-valid
// old store object when one exists (offline-capable), then reconcile from the
// lock — a committed hit relinks agents with no fetch; otherwise the clone
// cache or the network materializes the recorded commit.
func (a *App) migrateSkill(ctx context.Context, p *project, lf *skillslock.State, name string, locked skillslock.Record, opts migrateRunOptions) error {
	a.seedFromLegacyStore(p, name, locked.Resolved.ContentHash)

	agents, err := a.agentsByID(locked.Installation.Agents)
	if err != nil {
		return err
	}
	result, err := a.reconcileFromLock(ctx, p, name, locked, agents,
		SyncRequest{Root: p.root, Offline: opts.offline}, reconcileOpts{})
	if err != nil {
		return err
	}
	if opts.frozen {
		return nil
	}
	locked.Installation.Scope = "" // the retired store-scope field dies on rewrite
	applyInstallation(&locked, result)
	lf.Skills[name] = locked
	return nil
}

// seedFromLegacyStore copies a hash-valid object from a pre-022 store into
// the active entry so the following reconcile is a committed-content hit
// (no network). Best-effort: a missing or invalid object just falls through
// to cache/fetch. The old store itself is only ever read.
func (a *App) seedFromLegacyStore(p *project, name, hash string) {
	hex, ok := strings.CutPrefix(hash, "sha256:")
	if !ok {
		return
	}
	candidates := make([]string, 0, 4)
	if p.homeRoot != "" {
		candidates = append(candidates, filepath.Join(p.homeRoot, "store", "sha256", hex, "content"))
	}
	candidates = append(candidates,
		filepath.Join(p.root, stateDirName, "store", "sha256", hex),
		filepath.Join(p.root, stateDirName, "store", "sha256", hex, "content"),
	)
	for _, obj := range candidates {
		if info, err := os.Stat(obj); err != nil || !info.IsDir() {
			continue
		}
		if ok, _, err := integrity.VerifyDir(obj, hash); err != nil || !ok {
			continue
		}
		if _, err := active.EnsureActive(p.root, name, obj, active.EnsureOptions{
			ExpectedHash: hash,
			Replace:      true,
			LegacyRoots:  p.legacyStoreRoots(),
		}); err == nil {
			return
		}
	}
}

// legacyNoticePath names the old store the notice points at: the home store
// when it exists, else the project-local one.
func (a *App) legacyNoticePath(p *project) string {
	if p.homeRoot != "" {
		if home := filepath.Join(p.homeRoot, "store"); fileExists(home) {
			return home
		}
	}
	return filepath.Join(p.root, stateDirName, "store")
}

// ensureManifest generates skills.toml from the gskill-owned lock entries of a
// project installed before the manifest existed (spec 023 FR-015).
//
// Every pre-023 project is in this shape, and without generation overrides are
// unreachable: a user would have to hand-write a file whose schema they have
// never seen, for skills they already installed. The declarations come from
// the lock, so what is written is the intent the lock already recorded — the
// round-trip test (FR-016) is what holds that honest.
//
// Two carve-outs. A frozen run writes no declaration file: --frozen-lockfile
// means declarations do not change, and creating one is a change. A dry run
// writes nothing at all. Non-mutating commands never reach here.
func (a *App) ensureManifest(p *project, lf *skillslock.State, opts migrateRunOptions) error {
	if opts.frozen || opts.dryRun {
		return nil
	}
	if _, err := os.Stat(manifestPath(p.root)); err == nil {
		return nil
	}

	names := make([]string, 0, len(lf.Skills))
	for _, name := range sortedKeys(lf.Skills) {
		// Only gskill-owned entries are declared: skills-lock.json is shared
		// with other tools under spec 012, and their entries are neither ours
		// to describe nor ours to reinstall (FR-014).
		if lf.Skills[name].Resolved.ContentHash == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	if err := a.syncManifestSkills(p, lf, names, ""); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.notice, "generated %s from %d locked skill(s); edit it to declare overrides\n",
		manifest.FileName, len(names))
	return nil
}
