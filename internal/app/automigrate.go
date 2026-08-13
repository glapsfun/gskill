package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/integrity"
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
	var migrated []string
	for _, name := range sortedKeys(lf.Skills) {
		if opts.skip[name] {
			continue
		}
		locked := lf.Skills[name]
		if !a.isLegacySkill(p, name, locked) {
			continue
		}
		if err := a.migrateSkill(ctx, p, lf, name, locked, opts.frozen); err != nil {
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
func (a *App) migrateSkill(ctx context.Context, p *project, lf *skillslock.State, name string, locked skillslock.Record, frozen bool) error {
	a.seedFromLegacyStore(p, name, locked.Resolved.ContentHash)

	agents, err := a.agentsByID(locked.Installation.Agents)
	if err != nil {
		return err
	}
	result, err := a.reconcileFromLock(ctx, p, name, locked, agents, SyncRequest{Root: p.root}, false)
	if err != nil {
		return err
	}
	if frozen {
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
