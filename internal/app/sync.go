package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
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

// Sync reconciles the filesystem to the manifest's desired state across the
// three layers (store → active → agent). It installs declared-but-missing
// skills, creates declared-but-missing agent targets, and skips skills whose
// store, active entry, and agent targets already match — never re-resolving or
// re-downloading unchanged content (FR-010..FR-015). With Prune it removes agent
// targets and active entries the manifest no longer declares; without Prune it
// reports such orphans instead of deleting them (FR-013).
func (a *App) Sync(ctx context.Context, req SyncRequest) (SyncResult, error) {
	p := openProject(req.Root)
	if !p.manifestExists() {
		return SyncResult{}, errNoManifest()
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return SyncResult{}, err
	}

	var out SyncResult
	err = a.withLock(ctx, p, func() error {
		var rErr error
		out, rErr = a.reconcile(ctx, p, m, req)
		return rErr
	})
	if err != nil {
		return SyncResult{}, err
	}
	return out, nil
}

// reconcile performs the manifest-to-disk reconciliation under the project lock,
// returning the per-skill outcome plus prune/orphan results.
func (a *App) reconcile(ctx context.Context, p *project, m *manifest.Manifest, req SyncRequest) (SyncResult, error) {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return SyncResult{}, err
	}
	desired, err := a.desiredAgentSets(ctx, p, m)
	if err != nil {
		return SyncResult{}, err
	}

	out, lockChanged, manifestChanged, err := a.reconcileSkills(ctx, p, lf, m, desired, req)
	if err != nil {
		return SyncResult{}, err
	}

	if req.Prune {
		pruned, pErr := a.pruneToDesired(p, lf, m, desired)
		if pErr != nil {
			return SyncResult{}, pErr
		}
		out.Pruned = pruned
		lockChanged = lockChanged || len(pruned) > 0
	} else {
		out.Orphans = findOrphans(lf, m, desired)
	}

	out.UpToDate = !lockChanged && !manifestChanged && noChanges(out.Reconciled)
	if lockChanged {
		if err := lockfile.Save(p.lockPath, lf); err != nil {
			return SyncResult{}, err
		}
	}
	if manifestChanged {
		if err := manifest.Save(p.manifestPath, m); err != nil {
			return SyncResult{}, err
		}
	}
	return out, nil
}

// reconcileSkills reconciles every declared skill, returning the outcomes and
// whether the lockfile and/or manifest changed (the manifest changes when an
// always-present field is backfilled — 008 FR-009).
func (a *App) reconcileSkills(ctx context.Context, p *project, lf *lockfile.Lockfile, m *manifest.Manifest, desired map[string]map[string]bool, req SyncRequest) (SyncResult, bool, bool, error) {
	var out SyncResult
	lockChanged := false
	manifestChanged := false
	names := sortedKeys(m.Skills)
	for k, name := range names {
		sctx := stampSkill(ctx, name, k+1, len(names))
		change, lc, mc, rErr := a.reconcileSkill(sctx, p, lf, m, name, desired[name], req)
		if rErr != nil {
			return SyncResult{}, false, false, rErr
		}
		out.Reconciled = append(out.Reconciled, change)
		lockChanged = lockChanged || lc
		manifestChanged = manifestChanged || mc
	}
	return out, lockChanged, manifestChanged, nil
}

// desiredAgentSets resolves, per declared skill, the set of target agent IDs
// (explicit, defaults, or detected) the manifest asks for.
func (a *App) desiredAgentSets(ctx context.Context, p *project, m *manifest.Manifest) (map[string]map[string]bool, error) {
	out := make(map[string]map[string]bool, len(m.Skills))
	for name, ms := range m.Skills {
		agents, err := a.targetAgents(ctx, p.root, ms.Agents, m.Defaults.Agents)
		if err != nil {
			return nil, err
		}
		set := make(map[string]bool, len(agents))
		for _, ag := range agents {
			set[ag.ID()] = true
		}
		out[name] = set
	}
	return out, nil
}

// reconcileSkill brings one declared skill into the desired state. When the
// declaration is unchanged and the chain already matches, it makes no changes;
// otherwise it reconciles from the lockfile (no re-resolution) or re-resolves
// when the declaration itself changed. It also backfills the always-present
// manifest fields (008 FR-009), mutating m.Skills[name] in place and reporting
// whether the lockfile and/or manifest changed.
func (a *App) reconcileSkill(ctx context.Context, p *project, lf *lockfile.Lockfile, m *manifest.Manifest, name string, want map[string]bool, req SyncRequest) (SyncChange, bool, bool, error) {
	ms := m.Skills[name]
	hasDefaultsAgents := len(m.Defaults.Agents) > 0
	desiredAgents, err := a.agentsByID(sortedSetKeys(want))
	if err != nil {
		return SyncChange{}, false, false, err
	}
	desiredIDs := agentIDs(desiredAgents)
	locked, hasLock := lf.Skills[name]

	if hasLock && declarationUnchanged(ms, locked) {
		// Migrate a legacy/incomplete manifest to the pinned form using the
		// locked resolution — no re-resolve — keeping the lock's requested in
		// step so the next run stays idempotent (008 FR-009). Record the agent set
		// the run actually materializes (desiredIDs), not the stale locked set, so
		// a newly-detected agent is not later treated as an orphan (008 finding 2).
		backfilled := backfillPins(ms, revFromLock(locked.Resolved), desiredIDs, hasDefaultsAgents)
		manifestChanged := !reflect.DeepEqual(backfilled, ms)
		lockChanged := false
		if manifestChanged {
			m.Skills[name] = backfilled
			locked.Requested = requestedFromSkill(backfilled)
			lf.Skills[name] = locked
			lockChanged = true
		}
		needed, nErr := a.reconcileNeeded(p, name, locked, desiredIDs)
		if nErr != nil {
			return SyncChange{}, false, false, nErr
		}
		if !needed {
			return SyncChange{Name: name, ContentHash: locked.Resolved.ContentHash, Changed: manifestChanged}, lockChanged, manifestChanged, nil
		}
		added := subtract(desiredIDs, locked.Installation.Agents)
		result, rErr := a.reconcileFromLock(ctx, p, name, locked, desiredAgents, req, false)
		if rErr != nil {
			return SyncChange{}, false, false, rErr
		}
		applyInstallation(&locked, result)
		lf.Skills[name] = locked
		return SyncChange{Name: name, ContentHash: locked.Resolved.ContentHash, Changed: true, AgentsAdded: added}, true, manifestChanged, nil
	}

	before := lf.Skills[name].Installation.Agents
	// Install for the full desired agent set (explicit + [defaults] agents +
	// detected), passing it as the default so installOne does not fall back to
	// detection-only and silently drop a [defaults] agents entry on first sync.
	change, newMS, iErr := a.installOne(ctx, p, lf, name, ms, InstallRequest{Root: p.root, Offline: req.Offline}, desiredIDs, hasDefaultsAgents)
	if iErr != nil {
		return SyncChange{}, false, false, iErr
	}
	manifestChanged := false
	if change.ManifestChanged {
		m.Skills[name] = newMS
		manifestChanged = true
	}
	added := subtract(desiredIDs, before)
	return SyncChange{Name: name, ContentHash: change.ContentHash, Changed: true, AgentsAdded: added}, true, manifestChanged, nil
}

// reconcileNeeded reports whether the chain for the desired agents is anything
// other than fully healthy (cheap, no hashing).
func (a *App) reconcileNeeded(p *project, name string, locked lockfile.LockedSkill, desiredIDs []string) (bool, error) {
	storeRoot, err := filepath.Abs(p.store.Root())
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

// reconcileFromLock re-materializes a skill for the desired agents using the
// locked revision and content hash, without re-resolving. preserveForeign
// makes the installer fail closed on unowned destinations — set by the
// agent-add path (adding an agent must never clobber a user's content, spec
// 011 FR-016), left false by sync/repair whose contract is restoring drift.
func (a *App) reconcileFromLock(ctx context.Context, p *project, name string, locked lockfile.LockedSkill, desiredAgents []agent.Agent, req SyncRequest, preserveForeign bool) (installer.Result, error) {
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
func applyInstallation(locked *lockfile.LockedSkill, result installer.Result) {
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

// sortedSetKeys returns the keys of a string set in sorted order.
func sortedSetKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// pruneToDesired removes agent targets and active entries the manifest no longer
// declares: undesired agents on still-declared skills, skills dropped from the
// manifest entirely, and any managed orphans hand-created in agent dirs. It
// updates the lockfile to match and GCs unreferenced store content.
func (a *App) pruneToDesired(p *project, lf *lockfile.Lockfile, m *manifest.Manifest, desired map[string]map[string]bool) ([]string, error) {
	var pruned []string

	// Undesired agents on still-declared skills (directed removal).
	for _, name := range sortedKeys(m.Skills) {
		locked, ok := lf.Skills[name]
		if !ok {
			continue
		}
		dropped, labels, err := a.pruneSkillAgents(p, name, &locked, desired[name])
		if err != nil {
			return nil, err
		}
		if len(dropped) > 0 {
			lf.Skills[name] = locked
			pruned = append(pruned, labels...)
		}
	}

	// Skills removed from the manifest entirely (directed removal).
	for _, name := range sortedKeys(lf.Skills) {
		if _, ok := m.Skills[name]; ok {
			continue
		}
		dropped, err := a.dropSkill(p, lf.Skills[name], name)
		if err != nil {
			return nil, err
		}
		delete(lf.Skills, name)
		pruned = append(pruned, dropped...)
	}

	// Managed orphans hand-created in agent dirs, plus orphan active entries.
	desiredSkills := manifestSkillSet(m)
	scanned, err := a.pruneAgentTargets(p, desiredSkills, a.managedRoots(p))
	if err != nil {
		return nil, err
	}
	pruned = append(pruned, scanned...)
	activePruned, err := pruneActiveOrphans(p, desiredSkills)
	if err != nil {
		return nil, err
	}
	pruned = append(pruned, activePruned...)

	if _, err := p.store.GC(referencedHashes(lf)); err != nil {
		return nil, err
	}
	return pruned, nil
}

// pruneSkillAgents removes the recorded targets for agents not in want, updating
// the locked entry in place. It returns the removed agent IDs and the labels to
// report.
func (a *App) pruneSkillAgents(p *project, name string, locked *lockfile.LockedSkill, want map[string]bool) (dropped, labels []string, err error) {
	scope := locked.Installation.Scope
	var keep []string
	for _, id := range locked.Installation.Agents {
		if want[id] {
			keep = append(keep, id)
			continue
		}
		if target, ok := locked.Installation.Targets[id]; ok {
			deleted, rmErr := a.removeSafeTarget(p, scope, id, name, target)
			if rmErr != nil {
				return nil, nil, fmt.Errorf("prune %s target: %w", id, rmErr)
			}
			if deleted {
				labels = append(labels, id+":"+name)
			}
		}
		delete(locked.Installation.Targets, id)
		delete(locked.Installation.Modes, id)
		dropped = append(dropped, id)
	}
	if len(dropped) > 0 {
		locked.Installation.Agents = keep
	}
	return dropped, labels, nil
}

// dropSkill removes every recorded target (confined) and the active entry for a
// skill that is no longer declared.
func (a *App) dropSkill(p *project, locked lockfile.LockedSkill, name string) ([]string, error) {
	scope := locked.Installation.Scope
	var dropped []string
	for _, id := range sortedKeys(locked.Installation.Targets) {
		deleted, rmErr := a.removeSafeTarget(p, scope, id, name, locked.Installation.Targets[id])
		if rmErr != nil {
			return nil, fmt.Errorf("remove %s target: %w", id, rmErr)
		}
		if deleted {
			dropped = append(dropped, id+":"+name)
		}
	}
	if err := active.Remove(p.root, name); err != nil {
		return nil, err
	}
	dropped = append(dropped, active.Rel(name))
	sort.Strings(dropped)
	return dropped, nil
}

// findOrphans reports the agent targets and active entries that would be pruned
// (skills no longer declared, or undesired agents on declared skills) without
// removing anything.
func findOrphans(lf *lockfile.Lockfile, m *manifest.Manifest, desired map[string]map[string]bool) []string {
	var orphans []string
	for _, name := range sortedKeys(lf.Skills) {
		locked := lf.Skills[name]
		if _, ok := m.Skills[name]; !ok {
			for id := range locked.Installation.Targets {
				orphans = append(orphans, id+":"+name)
			}
			orphans = append(orphans, active.Rel(name))
			continue
		}
		want := desired[name]
		for _, id := range locked.Installation.Agents {
			if !want[id] {
				orphans = append(orphans, id+":"+name)
			}
		}
	}
	sort.Strings(orphans)
	return orphans
}

// managedRoots returns the absolute roots a gskill-managed target may link into.
func (a *App) managedRoots(p *project) []string {
	storeRoot, _ := filepath.Abs(p.store.Root())
	activeRoot, _ := filepath.Abs(active.Dir(p.root))
	return []string{activeRoot, storeRoot}
}

// manifestSkillSet returns the set of declared skill names.
func manifestSkillSet(m *manifest.Manifest) map[string]bool {
	set := make(map[string]bool, len(m.Skills))
	for name := range m.Skills {
		set[name] = true
	}
	return set
}

// pruneAgentTargets removes gskill-managed agent targets (symlinks into the
// active layer or, for legacy installs, the store) that the lockfile no longer
// references, leaving foreign paths intact.
func (a *App) pruneAgentTargets(p *project, locked map[string]bool, managedRoots []string) ([]string, error) {
	var pruned []string
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
			if locked[entry.Name()] {
				continue
			}
			target := filepath.Join(container, entry.Name())
			managed, mErr := managedBySymlink(target, managedRoots...)
			if mErr != nil {
				return nil, fmt.Errorf("inspect %s/%s: %w", ag.ID(), entry.Name(), mErr)
			}
			if !managed {
				continue
			}
			if rmErr := os.Remove(target); rmErr != nil {
				return nil, fmt.Errorf("prune %s/%s: %w", ag.ID(), entry.Name(), rmErr)
			}
			pruned = append(pruned, ag.ID()+":"+entry.Name())
		}
	}
	return pruned, nil
}

// pruneActiveOrphans removes active-layer entries the lockfile no longer
// references.
func pruneActiveOrphans(p *project, locked map[string]bool) ([]string, error) {
	names, err := active.List(p.root)
	if err != nil {
		return nil, err
	}
	var pruned []string
	for _, name := range names {
		if locked[name] {
			continue
		}
		if rmErr := active.Remove(p.root, name); rmErr != nil {
			return nil, rmErr
		}
		pruned = append(pruned, active.Rel(name))
	}
	return pruned, nil
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
