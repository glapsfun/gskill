package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/git"
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
	ctx = git.WithMemo(ctx)
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
	if err := a.autoMigrate(ctx, p, lf, migrateRunOptions{offline: req.Offline}); err != nil {
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
		// Whatever prune could not prove gskill's own is still on disk, so it
		// is still an orphan: report it rather than letting it vanish from
		// the output while it accumulates in the repo.
		remaining, oErr := a.findOrphans(p, lf)
		if oErr != nil {
			return SyncResult{}, oErr
		}
		out.Orphans = remaining
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
	// A symlink-less checkout is detected and reported, never silently
	// repaired (spec 022 FR-016): recreating real symlinks behind the user's
	// core.symlinks=false checkout would fight git on every status.
	for _, id := range desiredIDs {
		rel := locked.Installation.Targets[id]
		if rel != "" && isSymlinklessArtifact(resolveTarget(p.root, rel)) {
			return SyncChange{}, false, fmt.Errorf("%w: %s", errs.ErrInvalidLock, symlinklessCheckoutMsg(rel))
		}
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
	probe := locked
	probe.Installation.Agents = desiredIDs
	h, err := a.evaluateSkill(p, name, probe, false)
	if err != nil {
		return true, err
	}
	return !h.WithoutOverrideDrift().Healthy(), nil
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

	// The declared override is part of restoring: ExpectContentHash below is
	// the *post-override* hash, so re-materializing without re-applying the
	// override would hash the bare upstream and fail closed on its own
	// restore (spec 023 FR-017).
	spec, _, err := a.overrideFor(p.root, name)
	if err != nil {
		return installer.Request{}, err
	}

	home, _ := os.UserHomeDir()
	return installer.Request{
		Override:          spec,
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
		LegacyStoreRoots:  p.legacyStoreRoots(),
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
			if locked, ok := lf.Skills[name]; ok && slices.Contains(locked.Installation.Agents, ag.ID()) {
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
			// active.Remove refuses to delete content it cannot prove gskill
			// installed, and an orphan has no lock entry left to prove it
			// with — so a committed directory survives. Reporting it as
			// pruned would be a lie; leave it for the orphan list instead
			// (reconcile re-runs findOrphans after a prune).
			if _, statErr := os.Lstat(active.Path(p.root, name)); statErr == nil {
				continue
			}
		}
		found = append(found, active.Rel(name))
	}
	return found, nil
}

// managedRoots returns the absolute roots a gskill-managed target may link
// into: the repo's .agents/skills root (spec 022 — agent links are relative
// links resolving there). Legacy store links are deliberately NOT managed:
// they fail closed everywhere until migration converts them.
func (a *App) managedRoots(p *project) []string {
	activeRoot, _ := filepath.Abs(active.Dir(p.root))
	return []string{activeRoot}
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
