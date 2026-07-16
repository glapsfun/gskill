package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/installer"
)

// SyncRequest describes a `sync` invocation.
type SyncRequest struct {
	Root    string
	Prune   bool
	Offline bool
}

// SyncChange reports one skill's reconcile outcome.
type SyncChange struct {
	Name        string   `json:"name"`
	ContentHash string   `json:"content_hash"`
	Changed     bool     `json:"changed"`
	AgentsAdded []string `json:"agents_added,omitempty"`
}

// SyncResult reports a sync run.
type SyncResult struct {
	Reconciled []SyncChange
	Pruned     []string
	Orphans    []string
	UpToDate   bool
}

// Sync reconciles the filesystem to the lock's declared state across the
// three layers (store → active → agent). It restores declared-but-missing
// installs and skips skills whose store, active entry, and agent targets
// already match — never re-resolving or re-downloading unchanged content
// (FR-010..FR-015). With Prune it removes managed agent targets and active
// entries the lock no longer declares; without Prune it reports such orphans
// instead of deleting them (FR-013).
func (a *App) Sync(ctx context.Context, req SyncRequest) (SyncResult, error) {
	p, err := a.openProjectScoped(req.Root)
	if err != nil {
		return SyncResult{}, err
	}
	if !fileExists(p.lockPath) {
		// Without this gate a missing lock reads as "nothing declared" and
		// --prune would wipe every managed install.
		return SyncResult{}, errNoLock()
	}
	var out SyncResult
	err = a.withLock(ctx, p, func() error {
		var rErr error
		out, rErr = a.reconcile(ctx, p, req)
		if rErr == nil {
			if lf, lfErr := loadOrNewLock(p.lockPath); lfErr == nil {
				a.recordProjectState(ctx, p, lf)
			}
		}
		return rErr
	})
	if err != nil {
		return SyncResult{}, err
	}
	return out, nil
}

// reconcile performs the lock-to-disk reconciliation under the project lock,
// returning the per-skill outcome plus prune/orphan results.
func (a *App) reconcile(ctx context.Context, p *project, req SyncRequest) (SyncResult, error) {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return SyncResult{}, err
	}

	out, lockChanged, err := a.reconcileSkills(ctx, p, lf, req)
	if err != nil {
		return SyncResult{}, err
	}

	if req.Prune {
		pruned, pErr := a.pruneToDesired(p, lf)
		if pErr != nil {
			return SyncResult{}, pErr
		}
		out.Pruned = pruned
	} else {
		orphans, oErr := a.findOrphans(p, lf)
		if oErr != nil {
			return SyncResult{}, oErr
		}
		out.Orphans = orphans
	}

	out.UpToDate = !lockChanged && noChanges(out.Reconciled) && len(out.Pruned) == 0
	if lockChanged {
		if err := saveLock(p.lockPath, lf); err != nil {
			return SyncResult{}, err
		}
	}
	return out, nil
}

// reconcileSkills reconciles every locked skill, returning the outcomes and
// whether the lock changed.
func (a *App) reconcileSkills(ctx context.Context, p *project, lf *skillslock.State, req SyncRequest) (SyncResult, bool, error) {
	var out SyncResult
	lockChanged := false
	names := sortedKeys(lf.Skills)
	for k, name := range names {
		sctx := stampSkill(ctx, name, k+1, len(names))
		change, lc, rErr := a.reconcileSkill(sctx, p, lf, name, req)
		if rErr != nil {
			return SyncResult{}, false, rErr
		}
		out.Reconciled = append(out.Reconciled, change)
		lockChanged = lockChanged || lc
	}
	return out, lockChanged, nil
}

// reconcileSkill brings one locked skill into its declared state: the entry's
// recorded agents, mode, and revision. When the chain already matches it makes
// no changes; otherwise it re-materializes from the lock (no re-resolution).
func (a *App) reconcileSkill(ctx context.Context, p *project, lf *skillslock.State, name string, req SyncRequest) (SyncChange, bool, error) {
	locked := lf.Skills[name]
	desiredAgents, err := a.agentsByID(locked.Installation.Agents)
	if err != nil {
		return SyncChange{}, false, err
	}
	desiredIDs := agentIDs(desiredAgents)

	lockChanged := false
	if rq := backfillRequested(locked.Requested, revFromLock(locked.Resolved)); rq != locked.Requested {
		locked.Requested = rq
		lf.Skills[name] = locked
		lockChanged = true
	}

	needed, nErr := a.reconcileNeeded(p, name, locked, desiredIDs)
	if nErr != nil {
		return SyncChange{}, false, nErr
	}
	if !needed {
		return SyncChange{Name: name, ContentHash: locked.Resolved.ContentHash}, lockChanged, nil
	}
	result, rErr := a.reconcileFromLock(ctx, p, name, locked, desiredAgents, req, false)
	if rErr != nil {
		return SyncChange{}, false, rErr
	}
	applyInstallation(&locked, result)
	lf.Skills[name] = locked
	return SyncChange{Name: name, ContentHash: locked.Resolved.ContentHash, Changed: true}, true, nil
}

// reconcileNeeded reports whether the chain for the desired agents is anything
// other than fully healthy (cheap, no hashing).
func (a *App) reconcileNeeded(p *project, name string, locked skillslock.Record, desiredIDs []string) (bool, error) {
	storeRoot, err := filepath.Abs(p.contentRoot())
	if err != nil {
		return true, fmt.Errorf("resolve store root: %w", err)
	}
	probe := locked
	probe.Installation.Agents = desiredIDs
	h, err := a.evaluateSkill(p, name, probe, storeRoot, false)
	if err != nil {
		return true, err
	}
	return !h.Healthy(), nil
}

// frozenRequest builds an installer request that reproduces a locked skill
// exactly: locked source, revision, scope, mode, and expected content hash.
func (a *App) frozenRequest(p *project, name string, locked skillslock.Record, req InstallRequest) (installer.Request, error) {
	agents, err := a.agentsByID(locked.Installation.Agents)
	if err != nil {
		return installer.Request{}, err
	}

	ref := refFromLock(locked.Source)
	rev := revFromLock(locked.Resolved)

	home, _ := os.UserHomeDir()
	return installer.Request{
		Ref:               ref,
		Revision:          rev,
		Name:              name,
		Path:              ref.Path,
		Agents:            agents,
		Scope:             installer.Scope(locked.Installation.Scope),
		ModePref:          locked.Installation.Mode,
		ProjectRoot:       p.root,
		Home:              home,
		Offline:           req.Offline,
		ExpectContentHash: locked.Resolved.ContentHash,
	}, nil
}

// reconcileFromLock re-materializes a skill for the desired agents using the
// locked revision and content hash, without re-resolving. preserveForeign
// makes the installer fail closed on unowned destinations — set by the
// agent-add path (adding an agent must never clobber a user's content, spec
// 011 FR-016), left false by sync/repair whose contract is restoring drift.
func (a *App) reconcileFromLock(ctx context.Context, p *project, name string, locked skillslock.Record, desiredAgents []agent.Agent, req SyncRequest, preserveForeign bool) (installer.Result, error) {
	ireq, err := a.frozenRequest(p, name, locked, InstallRequest{Root: p.root, Offline: req.Offline})
	if err != nil {
		return installer.Result{}, err
	}
	ireq.Agents = desiredAgents
	ireq.PreserveForeign = preserveForeign
	ireq.PriorContentHash = locked.Resolved.ContentHash
	return a.installerForScope(p, string(ireq.Scope)).Install(ctx, ireq)
}

// applyInstallation copies an install result's placement facts onto a locked
// entry, preserving its resolution and provenance.
func applyInstallation(locked *skillslock.Record, result installer.Result) {
	locked.Installation.Mode = string(result.Mode)
	locked.Installation.Agents = result.Agents
	locked.Installation.ActivePath = result.ActivePath
	locked.Installation.Targets = result.Targets
	locked.Installation.Modes = result.Modes
}

// noChanges reports whether every reconciled skill was unchanged.
func noChanges(changes []SyncChange) bool {
	for _, c := range changes {
		if c.Changed {
			return false
		}
	}
	return true
}

// pruneToDesired removes managed installs the lock no longer declares —
// skills without an entry, and agents dropped from a still-declared entry's
// gskill.agents. Foreign content and external-only entries are never touched.
// It then GCs unreferenced store content, protecting content still reachable
// through an external entry's active link.
func (a *App) pruneToDesired(p *project, lf *skillslock.State) ([]string, error) {
	external, err := declaredExternalNames(p.lockPath, lf)
	if err != nil {
		return nil, err
	}
	pruned, err := a.sweepOrphans(p, lf, external, true)
	if err != nil {
		return nil, err
	}

	refs := referencedHashes(lf)
	if err := a.keepExternalActiveContent(p, external, refs); err != nil {
		return nil, err
	}
	// Project-local store GC only: pruning never deletes shared global
	// content (spec 015 FR-009/FR-024); for scope=global p.store is empty.
	if _, err := p.store.GC(refs); err != nil {
		return nil, err
	}
	return pruned, nil
}

// findOrphans reports what pruneToDesired would remove, without removing
// anything.
func (a *App) findOrphans(p *project, lf *skillslock.State) ([]string, error) {
	external, err := declaredExternalNames(p.lockPath, lf)
	if err != nil {
		return nil, err
	}
	return a.sweepOrphans(p, lf, external, false)
}

// declaredExternalNames returns the shared lock's external-only entry names —
// declared in the file but carrying no gskill block. gskill must never prune
// their installs: the entry still declares the skill even though another tool
// manages it.
func declaredExternalNames(lockPath string, lf *skillslock.State) (map[string]bool, error) {
	out := map[string]bool{}
	if !fileExists(lockPath) {
		return out, nil
	}
	l, err := skillslock.Load(lockPath)
	if err != nil {
		return nil, err
	}
	for _, name := range l.Names() {
		if _, managed := lf.Skills[name]; !managed {
			out[name] = true
		}
	}
	return out, nil
}

// sweepOrphans scans agent directories and the active layer for gskill-managed
// installs the lock no longer declares: skills with no entry, and agents no
// longer in a still-declared entry's recorded set. External-only entries are
// skipped entirely; foreign (non-symlink-managed) content is never touched.
// With remove=true the orphans are deleted, otherwise only reported.
func (a *App) sweepOrphans(p *project, lf *skillslock.State, external map[string]bool, remove bool) ([]string, error) {
	found, err := a.sweepAgentOrphans(p, lf, external, remove)
	if err != nil {
		return nil, err
	}
	activeFound, err := sweepActiveOrphans(p, lf, external, remove)
	if err != nil {
		return nil, err
	}
	found = append(found, activeFound...)
	sort.Strings(found)
	return found, nil
}

// sweepAgentOrphans scans every agent directory for managed symlinks whose
// (skill, agent) pair the lock no longer declares.
func (a *App) sweepAgentOrphans(p *project, lf *skillslock.State, external map[string]bool, remove bool) ([]string, error) {
	roots := a.managedRoots(p)
	var found []string
	for _, ag := range a.agents.All() {
		container := ag.ProjectSkillDir(p.root)
		entries, err := os.ReadDir(container)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s skills: %w", ag.ID(), err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if external[name] {
				continue
			}
			if locked, ok := lf.Skills[name]; ok && contains(locked.Installation.Agents, ag.ID()) {
				continue
			}
			target := filepath.Join(container, name)
			managed, mErr := managedBySymlink(target, roots...)
			if mErr != nil {
				return nil, fmt.Errorf("inspect %s/%s: %w", ag.ID(), name, mErr)
			}
			if !managed {
				continue
			}
			if remove {
				if rmErr := os.Remove(target); rmErr != nil {
					return nil, fmt.Errorf("prune %s/%s: %w", ag.ID(), name, rmErr)
				}
			}
			found = append(found, ag.ID()+":"+name)
		}
	}
	return found, nil
}

// sweepActiveOrphans scans the active layer for entries of skills the lock no
// longer declares.
func sweepActiveOrphans(p *project, lf *skillslock.State, external map[string]bool, remove bool) ([]string, error) {
	names, err := active.List(p.root)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, name := range names {
		if external[name] {
			continue
		}
		if _, ok := lf.Skills[name]; ok {
			continue
		}
		if remove {
			if rmErr := active.Remove(p.root, name); rmErr != nil {
				return nil, rmErr
			}
		}
		found = append(found, active.Rel(name))
	}
	return found, nil
}

// keepExternalActiveContent adds to refs the store content still reachable
// through an external-only entry's active symlink: gskill has no record for
// such entries, so the link itself is the reference that must survive GC.
// Refs resolve against the PROJECT-LOCAL store root — the store the callers'
// p.store.GC sweeps — never contentRoot(): under scope=global with a still-
// populated legacy store the two diverge, and resolving against the global
// root would leave every legacy-store link unprotected.
func (a *App) keepExternalActiveContent(p *project, external, refs map[string]bool) error {
	if len(external) == 0 {
		return nil
	}
	storeRoot, err := filepath.Abs(p.store.Root())
	if err != nil {
		return err
	}
	for name := range external {
		target, err := os.Readlink(active.Path(p.root, name))
		if err != nil {
			continue // absent or not a symlink: nothing in the store to protect
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(active.Dir(p.root), target)
		}
		rel, err := filepath.Rel(storeRoot, filepath.Clean(target))
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		// "sha256/hex" on disk ↔ "sha256:hex" content key.
		refs[strings.Replace(filepath.ToSlash(rel), "/", ":", 1)] = true
	}
	return nil
}

// managedRoots returns the absolute roots a gskill-managed target may link into.
func (a *App) managedRoots(p *project) []string {
	activeRoot, _ := filepath.Abs(active.Dir(p.root))
	// Both store roots are gskill-owned link targets: the resolved scope's
	// root plus the legacy project-local root, so links created before or
	// after a store-scope transition are both recognized (spec 015 FR-011).
	legacyRoot, _ := filepath.Abs(filepath.Join(p.root, stateDirName, "store"))
	roots := []string{activeRoot, legacyRoot}
	if resolved, err := filepath.Abs(p.contentRoot()); err == nil && resolved != legacyRoot {
		roots = append(roots, resolved)
	}
	return roots
}

// managedBySymlink reports whether path is a symlink that resolves into one of
// the gskill-managed roots (the active layer, or the store for legacy installs),
// i.e. an install gskill itself created. Plain directories and symlinks pointing
// elsewhere are treated as foreign and never pruned.
func managedBySymlink(path string, roots ...string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	target = filepath.Clean(target)
	for _, root := range roots {
		root = filepath.Clean(root)
		if target == root || strings.HasPrefix(target, root+string(filepath.Separator)) {
			return true, nil
		}
	}
	return false, nil
}
