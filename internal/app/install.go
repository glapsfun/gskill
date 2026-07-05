package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/selection"
	"github.com/glapsfun/gskill/internal/source"
)

const defaultLockTimeout = 30 * time.Second

// InitResult reports what Init created.
type InitResult struct {
	ManifestPath string
	Created      []string
}

// Init scaffolds the project: a gskill.toml manifest, a .gskill state dir, and
// a .gitignore hint (FR-001). It is idempotent.
func (a *App) Init(_ context.Context, root string) (InitResult, error) {
	p := openProject(root)
	res := InitResult{ManifestPath: p.manifestPath}

	if !p.manifestExists() {
		if err := manifest.Save(p.manifestPath, manifest.New()); err != nil {
			return InitResult{}, err
		}
		res.Created = append(res.Created, ManifestName)
	}
	if err := os.MkdirAll(filepath.Join(root, stateDirName), 0o750); err != nil {
		return InitResult{}, fmt.Errorf("create state dir: %w", err)
	}
	added, err := ensureGitignore(root)
	if err != nil {
		return InitResult{}, err
	}
	if added {
		res.Created = append(res.Created, ".gitignore")
	}
	return res, nil
}

// AddRequest describes an `add` invocation.
type AddRequest struct {
	Root    string
	Source  string
	Version string
	Ref     string
	Commit  string
	Agents  []string
	Force   bool
	Scope   string
	Mode    string

	// Selection (US2). Selectors are raw --skill values (incl. "*"); All maps
	// --all; Path is the --path disambiguator; ListOnly maps --list.
	Selectors   []string
	All         bool
	Path        string
	ListOnly    bool
	Interactive bool

	// Discovery filters (FR-012).
	MaxDepth int
	Include  []string
	Exclude  []string

	// Chooser, when set and Interactive, picks among multiple discovered skills.
	// It receives every in-scope skill (invalid ones shown but not selectable)
	// and returns the chosen subset. The CLI wires this to the TUI picker; the
	// app stays independent of the view layer (FR-021).
	Chooser func([]discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error)
}

// InstalledSkill reports one installed skill in a (possibly multi-skill) add.
type InstalledSkill struct {
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	ContentHash string            `json:"content_hash"`
	Targets     map[string]string `json:"targets"`
}

// AddResult reports the outcome of an add (one or more skills).
type AddResult struct {
	Installed []InstalledSkill
	Listed    []discovery.DiscoveredSkill // populated for --list (no install)
	Warnings  []string
}

// Add resolves, installs, and records a new skill, updating the manifest and
// lockfile. It errors on an already-declared key unless Force is set (FR-047),
// and writes nothing when no target agent is available (FR-029).
func (a *App) Add(ctx context.Context, req AddRequest) (AddResult, error) {
	p := openProject(req.Root)
	if !p.manifestExists() {
		return AddResult{}, errNoManifest()
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return AddResult{}, err
	}

	// Adding an agent to an already-installed skill is a local relink: reuse the
	// locked revision and existing store, with no resolve or network (FR-001).
	if res, handled, laErr := a.tryLocalAgentAdd(ctx, p, m, req); handled {
		return res, laErr
	}

	ref, err := source.Parse(req.Source)
	if err != nil {
		return AddResult{}, err
	}
	ref = promoteLocalGit(ref)

	rev, warnings, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
	})
	if err != nil {
		return AddResult{}, err
	}

	// Discovery and listing are read-only and do not need a target agent; agents
	// are resolved only when an install is actually about to happen (so `--list`
	// works even with no agents detected).
	ireq := a.installRequest(req.Root, ref, rev, nil, req.Scope, modeOr(req.Mode, m.Defaults.InstallMode))
	inst := a.installerForScope(p, string(ireq.Scope))
	scan, err := inst.DiscoverAll(ctx, ireq, discovery.Options{
		MaxDepth: req.MaxDepth, Include: req.Include, Exclude: req.Exclude,
	})
	if err != nil {
		return AddResult{}, err
	}

	if req.ListOnly {
		return AddResult{Listed: skillsInScope(scan, ref.Path), Warnings: warnings}, nil
	}

	// An install needs a target agent. Resolve it before any interactive
	// selection so the user is not asked to pick skills only to fail afterward.
	agents, err := a.targetAgents(ctx, req.Root, req.Agents, m.Defaults.Agents)
	if err != nil {
		return AddResult{}, err
	}
	ireq.Agents = agents

	selected, err := a.resolveSelection(scan, req, ref.Path)
	if err != nil {
		return AddResult{}, err
	}
	return a.installSelected(ctx, p, m, req, ref, rev, ireq, inst, selected, warnings)
}

// tryLocalAgentAdd handles a pure agent-add — adding agents to one or more
// already-locked skills from the same source — entirely from the lockfile and
// store, with no resolver or network call (FR-001, review F7). It returns
// handled=true when it took ownership of the request (success or a conflict
// error); handled=false means the caller should fall through to the normal
// resolve+install path.
func (a *App) tryLocalAgentAdd(ctx context.Context, p *project, m *manifest.Manifest, req AddRequest) (AddResult, bool, error) {
	if disqualifiesLocalAdd(req) {
		return AddResult{}, false, nil
	}
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return AddResult{}, false, nil //nolint:nilerr // fall back to the normal path on a lock read error
	}
	targets, ok := localAgentAddTargets(m, lf, req)
	if !ok {
		return AddResult{}, false, nil
	}

	agents, err := a.targetAgents(ctx, req.Root, req.Agents, m.Defaults.Agents)
	if err != nil {
		return AddResult{}, true, err
	}
	reqIDs := agentIDs(agents)
	if !anyNewAgent(lf, targets, reqIDs) {
		return AddResult{}, true, conflictErr(targets[0])
	}

	res, err := a.materializeLocalAgentAdd(ctx, p, m, targets, reqIDs)
	return res, true, err
}

// disqualifiesLocalAdd reports whether a request can't be a pure agent-add (it
// changes the pin, only lists, or selects everything).
func disqualifiesLocalAdd(req AddRequest) bool {
	return req.Force || req.ListOnly || req.All ||
		req.Version != "" || req.Ref != "" || req.Commit != ""
}

// anyNewAgent reports whether any target skill gains a not-yet-installed agent.
func anyNewAgent(lf *lockfile.Lockfile, targets, reqIDs []string) bool {
	for _, name := range targets {
		if len(subtract(reqIDs, lf.Skills[name].Installation.Agents)) > 0 {
			return true
		}
	}
	return false
}

// materializeLocalAgentAdd relinks the new agents for every target skill under
// the project lock, with no resolution.
func (a *App) materializeLocalAgentAdd(ctx context.Context, p *project, m *manifest.Manifest, targets, reqIDs []string) (AddResult, error) {
	res := AddResult{}
	err := a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		for _, name := range targets {
			if relErr := a.relinkAgents(ctx, p, m, lf, name, reqIDs, &res); relErr != nil {
				return relErr
			}
		}
		if saveErr := manifest.Save(p.manifestPath, m); saveErr != nil {
			return saveErr
		}
		return lockfile.Save(p.lockPath, lf)
	})
	return res, err
}

// relinkAgents activates the not-yet-installed agents for one locked skill from
// the lock (no resolve) and merges the result into the manifest and lockfile.
func (a *App) relinkAgents(ctx context.Context, p *project, m *manifest.Manifest, lf *lockfile.Lockfile, name string, reqIDs []string, res *AddResult) error {
	locked := lf.Skills[name]
	newIDs := subtract(reqIDs, locked.Installation.Agents)
	if len(newIDs) == 0 {
		return nil
	}
	newAgents, err := a.agentsByID(newIDs)
	if err != nil {
		return err
	}
	result, err := a.reconcileFromLock(ctx, p, name, locked, newAgents, SyncRequest{Root: p.root})
	if err != nil {
		return err
	}
	mergeAgentInstall(&locked, result)

	ms := m.Skills[name]
	ms.Agents = locked.Installation.Agents
	// Backfill the version pin from the locked revision (no re-resolve) so an
	// agent-add to a pre-008 unpinned skill still records a version, and keep the
	// lock's requested in step (008 finding 3).
	ms = backfillPins(ms, revFromLock(locked.Resolved), ms.Agents, len(m.Defaults.Agents) > 0)
	m.Skills[name] = ms
	locked.Requested = requestedFromSkill(ms)
	lf.Skills[name] = locked

	res.Installed = append(res.Installed, InstalledSkill{
		Name: name, Path: ms.Path, ContentHash: locked.Resolved.ContentHash, Targets: result.Targets,
	})
	return nil
}

// localAgentAddTargets returns the already-locked skill names a pure agent-add
// targets, or ok=false when the request is not a pure agent-add (globby/`*`/path
// selectors, a source that is not uniquely locked, or any target whose source
// differs from the request).
func localAgentAddTargets(m *manifest.Manifest, lf *lockfile.Lockfile, req AddRequest) ([]string, bool) {
	var names []string
	if len(req.Selectors) > 0 {
		for _, sel := range req.Selectors {
			if strings.ContainsAny(sel, "*@/") {
				return nil, false
			}
			names = append(names, sel)
		}
	} else {
		var cands []string
		for name := range lf.Skills {
			if ms, in := m.Skills[name]; in && ms.Source == req.Source {
				cands = append(cands, name)
			}
		}
		if len(cands) != 1 {
			return nil, false
		}
		names = cands
	}

	for _, name := range names {
		ms, inM := m.Skills[name]
		_, inL := lf.Skills[name]
		if !inM || !inL || ms.Source != req.Source {
			return nil, false
		}
	}
	sort.Strings(names)
	return names, true
}

// resolveSelection turns the discovered skills into the set to install, honoring
// explicit selectors or, when none are given, the single-skill auto behavior.
func (a *App) resolveSelection(scan discovery.Result, req AddRequest, explicitPath string) ([]discovery.DiscoveredSkill, error) {
	sels, err := selection.Parse(req.Selectors, req.All, req.Path)
	if err != nil {
		return nil, err
	}
	if len(sels) > 0 {
		selected, resErr := selection.Resolve(scan, sels, req.Interactive)
		if resErr != nil {
			return nil, mapSelectionErr(resErr)
		}
		return selected, nil
	}

	valid, invalid := partitionScope(scan, explicitPath)
	switch {
	case len(valid) == 1:
		return valid, nil
	case len(valid) > 1:
		if req.Interactive && req.Chooser != nil {
			return a.chooseInteractive(req, skillsInScope(scan, explicitPath))
		}
		return nil, fmt.Errorf(
			"%w: source contains %d skills; select with --skill <name>, repeated --skill, or --skill '*'",
			errs.ErrUsage, len(valid))
	case len(invalid) > 0:
		return nil, fmt.Errorf("%w: skill %q is invalid: %s",
			errs.ErrInvalidManifest, invalid[0].ID, firstProblem(invalid[0]))
	default:
		return nil, fmt.Errorf("%w: no SKILL.md found in source", errs.ErrSourceUnavailable)
	}
}

// chooseInteractive delegates to the request's Chooser (the TUI picker) and
// treats an empty selection as a usage error so nothing is installed by mistake.
func (a *App) chooseInteractive(req AddRequest, candidates []discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error) {
	chosen, err := req.Chooser(candidates)
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("%w: no skill selected", errs.ErrUsage)
	}
	return chosen, nil
}

// installSelected installs the chosen skills atomically. For a skill already
// declared from the same source, a new target agent unions into the existing
// install (reusing the one store + active entry and adding only the missing
// agent target, FR-001..FR-005); the same skill with no new agent still
// conflicts, and the same name from a different source is a collision (FR-029).
// It stages and activates each skill, rolling back already-activated targets on
// any failure, and commits the manifest and lockfile only after all succeed.
func (a *App) installSelected(ctx context.Context, p *project, m *manifest.Manifest, req AddRequest, ref source.Ref, rev resolver.Revision, ireq installer.Request, inst *installer.Installer, selected []discovery.DiscoveredSkill, warnings []string) (AddResult, error) {
	res := AddResult{Warnings: append([]string(nil), warnings...)}
	reqIDs := agentIDs(ireq.Agents)
	err := a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		var activated []installer.Result
		rollback := func() {
			for _, r := range activated {
				a.removeTargets(req.Root, ireq.Scope, r)
			}
		}

		for _, s := range selected {
			plan, planErr := a.planAdd(m, lf, s.ID, req, reqIDs)
			if planErr != nil {
				rollback()
				return planErr
			}
			ir := ireq
			ir.Name = s.ID
			ir.Path = s.RepoPath
			ir.Agents = plan.activate
			result, instErr := inst.Install(ctx, ir)
			if instErr != nil {
				rollback()
				return instErr
			}
			activated = append(activated, result)
			if plan.mergeInto {
				locked := lf.Skills[s.ID]
				mergeAgentInstall(&locked, result)
				ms := m.Skills[s.ID]
				ms.Agents = plan.manifestIDs
				// Backfill the version pin from the already-locked revision (not the
				// freshly-resolved rev, so the pin matches what is locked), keeping
				// the lock's requested consistent (008 finding 3).
				ms = backfillPins(ms, revFromLock(locked.Resolved), ms.Agents, len(m.Defaults.Agents) > 0)
				m.Skills[s.ID] = ms
				locked.Requested = requestedFromSkill(ms)
				lf.Skills[s.ID] = locked
			} else {
				entry := manifestEntry(req, s.RepoPath, plan.manifestIDs)
				// Record the resolved version pin (and, when applicable, the
				// agent set) before building the lock entry, so lockfile
				// `requested` matches the manifest and sync stays idempotent
				// (008 FR-004/FR-007). Agents are already decided by planAdd.
				entry = backfillPins(entry, rev, plan.manifestIDs, len(m.Defaults.Agents) > 0)
				m.Skills[s.ID] = entry
				lf.Skills[s.ID] = buildLockEntry(ref, rev, ir, result, m.Skills[s.ID])
			}
			res.Installed = append(res.Installed, InstalledSkill{
				Name: s.ID, Path: s.RepoPath, ContentHash: result.ContentHash, Targets: result.Targets,
			})
			res.Warnings = append(res.Warnings, result.Warnings...)
		}

		if saveErr := manifest.Save(p.manifestPath, m); saveErr != nil {
			rollback()
			return saveErr
		}
		if saveErr := lockfile.Save(p.lockPath, lf); saveErr != nil {
			rollback()
			return saveErr
		}
		return nil
	})
	if err != nil {
		return AddResult{}, err
	}
	return res, nil
}

// addPlan is the agent set to activate now for one selected skill plus the agent
// IDs to persist in the manifest. mergeInto marks an agent-add (the skill is
// already installed), so only the new agents are activated and the result is
// merged into the existing lock entry rather than replacing it.
type addPlan struct {
	activate    []agent.Agent // agents to activate now (only the new ones for an agent-add)
	manifestIDs []string      // agent IDs to record in the manifest entry
	mergeInto   bool
}

// planAdd decides how a selected skill installs given the existing manifest and
// lockfile state, failing closed on a genuine conflict or cross-source collision.
func (a *App) planAdd(m *manifest.Manifest, lf *lockfile.Lockfile, id string, req AddRequest, reqIDs []string) (addPlan, error) {
	existing, exists := m.Skills[id]
	if !exists || req.Force {
		ags, err := a.agentsByID(reqIDs)
		return addPlan{activate: ags, manifestIDs: manifestAgentIDs(m, req, reqIDs)}, err
	}

	if existing.Source != req.Source {
		return addPlan{}, fmt.Errorf(
			"%w: skill %q is already declared from a different source %q (name collision); use a different name or 'gskill remove %s' first",
			errs.ErrInvalidManifest, id, existing.Source, id)
	}

	current := installedAgentIDs(lf, id, existing)
	newOnes := subtract(reqIDs, current)
	if len(newOnes) == 0 {
		return addPlan{}, conflictErr(id)
	}

	// Activate only the new agents; the existing targets are untouched and the
	// result is merged into the lock entry (FR-001..FR-005).
	ags, err := a.agentsByID(newOnes)
	return addPlan{activate: ags, manifestIDs: unionStrings(current, reqIDs), mergeInto: true}, err
}

// manifestAgentIDs decides which agent IDs to persist in a fresh manifest entry
// (008 FR-001/FR-003): explicit --agent values are recorded as resolved; with no
// --agent, the resolved set is recorded per-skill unless a project-wide
// [defaults] agents block is present, in which case the entry stays empty and
// keeps inheriting from that block.
func manifestAgentIDs(m *manifest.Manifest, req AddRequest, reqIDs []string) []string {
	if len(req.Agents) > 0 {
		return reqIDs
	}
	if len(m.Defaults.Agents) > 0 {
		return nil
	}
	return reqIDs
}

// conflictErr reports an already-declared skill with no new agent to add.
func conflictErr(id string) error {
	return fmt.Errorf("%w: skill %q already declared; use 'gskill update %s' or --force",
		errs.ErrInvalidManifest, id, id)
}

// mergeAgentInstall folds an agent-add install result into an existing lock
// entry, unioning agents and merging the per-agent target/mode records while
// preserving the resolved revision, source, and active path.
func mergeAgentInstall(locked *lockfile.LockedSkill, result installer.Result) {
	locked.Installation.Agents = unionStrings(locked.Installation.Agents, result.Agents)
	if locked.Installation.Targets == nil {
		locked.Installation.Targets = make(map[string]string, len(result.Targets))
	}
	for k, v := range result.Targets {
		locked.Installation.Targets[k] = v
	}
	if locked.Installation.Modes == nil {
		locked.Installation.Modes = make(map[string]string, len(result.Modes))
	}
	for k, v := range result.Modes {
		locked.Installation.Modes[k] = v
	}
	if locked.Installation.ActivePath == "" {
		locked.Installation.ActivePath = result.ActivePath
	}
}

// installedAgentIDs returns the agents a skill is currently installed for,
// preferring the lockfile's recorded set and falling back to the manifest.
func installedAgentIDs(lf *lockfile.Lockfile, id string, ms manifest.Skill) []string {
	if locked, ok := lf.Skills[id]; ok && len(locked.Installation.Agents) > 0 {
		return locked.Installation.Agents
	}
	return ms.Agents
}

// removeTargets best-effort removes a skill's activated directories during an
// atomic-install rollback.
func (a *App) removeTargets(root string, scope installer.Scope, r installer.Result) {
	for _, target := range r.Targets {
		path := target
		if scope != installer.ScopeGlobal {
			path = filepath.Join(root, target)
		}
		_ = os.RemoveAll(path)
	}
}

// mapSelectionErr maps a selection error to a gskill exit code: an invalid
// explicit selection is exit 3; ambiguity and no-match are usage errors (exit 2).
func mapSelectionErr(err error) error {
	if errors.Is(err, selection.ErrInvalidSelection) {
		return errs.Wrap(errs.CodeInvalidManifest, err.Error(), err)
	}
	return errs.Wrap(errs.CodeUsage, err.Error(), err)
}

// partitionScope splits the discovered skills, scoped to an optional explicit
// in-repo path, into valid and invalid sets.
func partitionScope(scan discovery.Result, explicitPath string) (valid, invalid []discovery.DiscoveredSkill) {
	for _, s := range scan.Skills {
		if explicitPath != "" && s.RepoPath != explicitPath {
			continue
		}
		if s.Valid {
			valid = append(valid, s)
		} else {
			invalid = append(invalid, s)
		}
	}
	return valid, invalid
}

// skillsInScope returns the discovered skills, scoped to an optional explicit
// in-repo path, for --list output.
func skillsInScope(scan discovery.Result, explicitPath string) []discovery.DiscoveredSkill {
	if explicitPath == "" {
		return scan.Skills
	}
	var out []discovery.DiscoveredSkill
	for _, s := range scan.Skills {
		if s.RepoPath == explicitPath {
			out = append(out, s)
		}
	}
	return out
}

// firstProblem returns the first error-severity diagnostic message for a skill.
func firstProblem(s discovery.DiscoveredSkill) string {
	for _, p := range s.Problems {
		if p.Severity == discovery.SeverityError {
			return p.Message
		}
	}
	return "unknown validation error"
}

// InstallRequest describes an `install` invocation over the existing manifest.
type InstallRequest struct {
	Root           string
	Scope          string
	Mode           string
	Frozen         bool
	Offline        bool
	NoCache        bool
	UpdateLockfile bool
}

// SkillChange records the per-skill outcome of an install.
type SkillChange struct {
	Name        string
	ContentHash string
	Changed     bool
	// ManifestChanged is set when an always-present field (agent set or version
	// pin) was backfilled into the manifest entry this run, so the caller knows
	// to persist gskill.toml (008 FR-008/FR-009). In-memory only.
	ManifestChanged bool
}

// InstallResult reports an install run.
type InstallResult struct {
	Skills  []SkillChange
	Changed bool
}

// Install materializes every declared skill, additively and idempotently,
// updating the lockfile only when resolved content changes (FR-022).
func (a *App) Install(ctx context.Context, req InstallRequest) (InstallResult, error) {
	if req.Frozen {
		return a.installFrozen(ctx, req)
	}

	p := openProject(req.Root)
	if !p.manifestExists() {
		return InstallResult{}, errNoManifest()
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return InstallResult{}, err
	}

	var out InstallResult
	err = a.withLock(ctx, p, func() error {
		var fnErr error
		out, fnErr = a.installAll(ctx, p, m, req)
		return fnErr
	})
	if err != nil {
		return InstallResult{}, err
	}
	return out, nil
}

// installAll materializes every declared skill under the held project lock,
// backfilling the always-present manifest fields and persisting the lockfile and
// manifest only when something changed (008 FR-007/FR-008).
func (a *App) installAll(ctx context.Context, p *project, m *manifest.Manifest, req InstallRequest) (InstallResult, error) {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return InstallResult{}, err
	}
	var out InstallResult
	manifestChanged := false
	for _, name := range sortedKeys(m.Skills) {
		change, newMS, applyErr := a.installOne(ctx, p, lf, name, m.Skills[name], req, m.Defaults.Agents, len(m.Defaults.Agents) > 0)
		if applyErr != nil {
			return InstallResult{}, applyErr
		}
		if change.ManifestChanged {
			m.Skills[name] = newMS
			manifestChanged = true
		}
		out.Skills = append(out.Skills, change)
		out.Changed = out.Changed || change.Changed
	}
	// Save the lockfile when content changed or a manifest pin was backfilled
	// (the latter updates the lock's `requested` to match — 008 FR-007).
	if out.Changed || manifestChanged {
		if err := lockfile.Save(p.lockPath, lf); err != nil {
			return InstallResult{}, err
		}
	}
	if manifestChanged {
		if err := manifest.Save(p.manifestPath, m); err != nil {
			return InstallResult{}, err
		}
	}
	return out, nil
}

// installOne installs a single declared skill and updates lf in place. It
// returns the (possibly backfilled) manifest entry so the caller can persist the
// always-present fields; SkillChange.ManifestChanged reports whether it differs
// from the input. hasDefaultsAgents suppresses per-skill agent recording when a
// [defaults] agents block governs the set (008 FR-001..FR-009).
func (a *App) installOne(ctx context.Context, p *project, lf *lockfile.Lockfile, name string, ms manifest.Skill, req InstallRequest, defaults []string, hasDefaultsAgents bool) (SkillChange, manifest.Skill, error) {
	ref, err := source.Parse(ms.Source)
	if err != nil {
		return SkillChange{}, ms, err
	}
	ref = promoteLocalGit(ref)
	agents, err := a.targetAgents(ctx, p.root, ms.Agents, defaults)
	if err != nil {
		return SkillChange{}, ms, err
	}
	rev, _, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{
		Version: ms.Version, Ref: ms.Ref, Commit: ms.Commit,
	})
	if err != nil {
		return SkillChange{}, ms, err
	}

	// Backfill the always-present fields before building the lock entry, so the
	// lockfile's `requested` is derived from the same pinned manifest entry and
	// declarationUnchanged stays satisfied on the next run (008 FR-007).
	backfilled := backfillPins(ms, rev, agentIDs(agents), hasDefaultsAgents)

	ireq := a.installRequest(p.root, ref, rev, agents, req.Scope, modeOr(req.Mode, ms.InstallMode))
	ireq.Name = name
	ireq.Offline = req.Offline
	// Honor the manifest's in-repo path so a multi-skill source resolves to the
	// declared skill instead of erroring on multiple SKILL.md files.
	if ms.Path != "" {
		ireq.Path = ms.Path
	}
	result, err := a.installerForScope(p, string(ireq.Scope)).Install(ctx, ireq)
	if err != nil {
		return SkillChange{}, ms, err
	}

	old, existed := lf.Skills[name]
	changed := !existed || old.Resolved.ContentHash != result.ContentHash || old.Resolved.Commit != rev.Commit
	lf.Skills[name] = buildLockEntry(ref, rev, ireq, result, backfilled)
	return SkillChange{
		Name: name, ContentHash: result.ContentHash, Changed: changed,
		ManifestChanged: !reflect.DeepEqual(backfilled, ms),
	}, backfilled, nil
}

// targetAgents resolves the agents to install into: explicit, then defaults,
// then detected; none available is exit 9 (FR-029, FR-030).
func (a *App) targetAgents(ctx context.Context, root string, explicit, defaults []string) ([]agent.Agent, error) {
	ids := explicit
	if len(ids) == 0 {
		ids = defaults
	}
	if len(ids) > 0 {
		out := make([]agent.Agent, 0, len(ids))
		for _, id := range ids {
			ag, ok := a.agents.Get(id)
			if !ok {
				return nil, errs.WithHint(
					fmt.Errorf("%w: unknown agent %q", errs.ErrUnsupportedAgent, id),
					"run 'gskill doctor' to list detected agents")
			}
			out = append(out, ag)
		}
		return out, nil
	}

	detected, err := a.agents.Detect(ctx, root)
	if err != nil {
		return nil, err
	}
	if len(detected) > 0 {
		return detected, nil
	}

	// Nothing specified or detected: default to Claude so installs work out of
	// the box.
	if def, ok := a.agents.Get(agent.DefaultID); ok {
		return []agent.Agent{def}, nil
	}
	known := make([]string, 0)
	for _, ag := range a.agents.All() {
		known = append(known, ag.ID())
	}
	return nil, errs.WithHint(
		fmt.Errorf("%w: no target agent specified and none detected (known: %s)",
			errs.ErrUnsupportedAgent, strings.Join(known, ", ")),
		"pass --agent <id>, or run 'gskill doctor' to see why detection found nothing")
}

// installRequest assembles an installer.Request with shared defaults.
func (a *App) installRequest(root string, ref source.Ref, rev resolver.Revision, agents []agent.Agent, scope, mode string) installer.Request {
	home, _ := os.UserHomeDir()
	return installer.Request{
		Ref:         ref,
		Revision:    rev,
		Path:        ref.Path,
		Agents:      agents,
		Scope:       scopeOr(scope),
		ModePref:    modeOr(mode, ""),
		ProjectRoot: root,
		Home:        home,
	}
}

// withLock runs fn while holding the project's exclusive mutate lock (FR-021).
func (a *App) withLock(ctx context.Context, p *project, fn func() error) error {
	if err := os.MkdirAll(p.locksDir, 0o750); err != nil {
		return fmt.Errorf("create locks dir: %w", err)
	}
	lock, err := fsutil.Acquire(ctx, filepath.Join(p.locksDir, "mutate.lock"), fsutil.LockExclusive, defaultLockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return fn()
}

// backfillPins fills the always-present manifest fields from the resolved
// revision so a committed gskill.toml records what was installed even when the
// user named no version or agent (008 FR-001..FR-006). It fills a version pin
// only when the user specified none, mapping by ref-kind: semver→caret range
// (e.g. ^0.1.0, kept floating so updates are not frozen), (non-semver) tag→ref,
// branch→ref, commit→commit; a local source has no resolvable version and is
// left unpinned. It records the resolved agent set
// per-skill unless the set is empty and a project-wide [defaults] agents block
// is present, in which case the entry keeps inheriting from that block. Explicit
// values are never overwritten.
func backfillPins(ms manifest.Skill, rev resolver.Revision, resolvedAgentIDs []string, hasDefaultsAgents bool) manifest.Skill {
	if ms.Version == "" && ms.Ref == "" && ms.Commit == "" {
		switch rev.RefKind {
		case resolver.RefKindSemver:
			// Record a caret range, not the bare resolved version, so the manifest
			// constraint that `outdated`/`update` read stays floating (within the
			// major) instead of freezing the skill at an exact pin (008 finding 1).
			ms.Version = "^" + rev.Version
		case resolver.RefKindTag:
			ms.Ref = rev.Tag
		case resolver.RefKindBranch:
			ms.Ref = rev.Branch
		case resolver.RefKindCommit:
			ms.Commit = rev.Commit
		case resolver.RefKindLocal:
			// No resolvable version; leave the entry unpinned (FR-006).
		}
	}
	if len(ms.Agents) == 0 && !hasDefaultsAgents {
		ms.Agents = resolvedAgentIDs
	}
	return ms
}

// requestedFromSkill mirrors a manifest entry's pin fields into a lockfile
// Requested record, so the lock's requested constraint stays consistent with the
// manifest after a backfill and declarationUnchanged keeps holding (008 FR-007).
func requestedFromSkill(ms manifest.Skill) lockfile.Requested {
	return lockfile.Requested{Version: ms.Version, Ref: ms.Ref, Commit: ms.Commit}
}

// manifestEntry builds the manifest record for an add (intent only). The path
// is the selected skill's in-repo location, so the manifest pins which skill
// inside the source was installed (FR-028/FR-030). agentIDs is the agent set to
// declare: the raw --agent values for a fresh add, or the unioned set when an
// agent is added to an already-declared skill.
func manifestEntry(req AddRequest, repoPath string, agentIDs []string) manifest.Skill {
	return manifest.Skill{
		Source:      req.Source,
		Path:        repoPath,
		Version:     req.Version,
		Ref:         req.Ref,
		Commit:      req.Commit,
		Agents:      agentIDs,
		InstallMode: req.Mode,
	}
}

// agentIDs extracts the IDs of the given agents in order.
func agentIDs(agents []agent.Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, ag := range agents {
		ids = append(ids, ag.ID())
	}
	return ids
}

// unionStrings returns the ordered union of a and b, preserving a's order then
// appending new values from b.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, vs := range [][]string{a, b} {
		for _, v := range vs {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// subtract returns the values in a that are not in b.
func subtract(a, b []string) []string {
	exclude := make(map[string]bool, len(b))
	for _, v := range b {
		exclude[v] = true
	}
	var out []string
	for _, v := range a {
		if !exclude[v] {
			out = append(out, v)
		}
	}
	return out
}

// buildLockEntry assembles the lockfile record from resolution + install reality.
func buildLockEntry(ref source.Ref, rev resolver.Revision, ireq installer.Request, result installer.Result, intent manifest.Skill) lockfile.LockedSkill {
	now := time.Now().UTC().Format(time.RFC3339)
	fm := result.Skill.Frontmatter
	resolved := lockfile.Resolved{
		Version:       rev.Version,
		RefKind:       string(rev.RefKind),
		Tag:           rev.Tag,
		Branch:        rev.Branch,
		Commit:        rev.Commit,
		ContentHash:   result.ContentHash,
		SkillFileHash: result.SkillFileHash,
		MutableRef:    rev.MutableRef,
	}
	if rev.RefKind == resolver.RefKindLocal {
		resolved.LocalPathHash = result.ContentHash
	}
	return lockfile.LockedSkill{
		Source: lockfile.Source{
			Type: string(ref.Type), Original: ref.Original, URL: ref.URL,
			Owner: ref.Owner, Repo: ref.Repo, Path: ireq.Path,
		},
		Requested: lockfile.Requested{Version: intent.Version, Ref: intent.Ref, Commit: intent.Commit},
		Resolved:  resolved,
		Metadata: lockfile.Metadata{
			Name: fm.Name, Description: fm.Description, Version: fm.Version, License: fm.License,
		},
		Requires: lockfile.Requires{
			Skills: fm.Requires.Skills, Commands: fm.Requires.Commands,
			Environment: fm.Requires.Environment, MCP: fm.Requires.MCP,
		},
		Installation: lockfile.Installation{
			Scope: string(ireq.Scope), Mode: string(result.Mode),
			Agents: result.Agents, ActivePath: result.ActivePath,
			Targets: result.Targets, Modes: result.Modes,
		},
		Provenance: lockfile.Provenance{FetchedAt: now, UpdatedAt: now, Trust: "checksum-ok"},
	}
}

// promoteLocalGit upgrades a local path that is a git repo to a git source so it
// resolves tags and commits like a remote.
func promoteLocalGit(ref source.Ref) source.Ref {
	if ref.Type != source.TypeLocal {
		return ref
	}
	if _, err := os.Stat(filepath.Join(ref.LocalPath, ".git")); err != nil {
		return ref
	}
	abs, err := filepath.Abs(ref.LocalPath)
	if err != nil {
		return ref
	}
	ref.Type = source.TypeGit
	ref.URL = abs
	ref.Repo = filepath.Base(abs)
	return ref
}

// refFromLock reconstructs a source.Ref from a locked source record.
func refFromLock(src lockfile.Source) source.Ref {
	ref := source.Ref{
		Type:     source.Type(src.Type),
		Original: src.Original,
		URL:      src.URL,
		Owner:    src.Owner,
		Repo:     src.Repo,
		Path:     src.Path,
	}
	if ref.Type == source.TypeLocal {
		ref.LocalPath = src.Original
	}
	return ref
}

// revFromLock reconstructs a resolver.Revision from a locked resolution record.
func revFromLock(res lockfile.Resolved) resolver.Revision {
	return resolver.Revision{
		RefKind:    resolver.RefKind(res.RefKind),
		Version:    res.Version,
		Tag:        res.Tag,
		Branch:     res.Branch,
		Commit:     res.Commit,
		MutableRef: res.MutableRef,
	}
}

// loadOrNewLock loads the lockfile at path, or returns a fresh one if absent.
func loadOrNewLock(path string) (*lockfile.Lockfile, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return lockfile.New(), nil
		}
		return nil, fmt.Errorf("stat lockfile: %w", err)
	}
	return lockfile.Load(path)
}

// gskillIgnorePatterns are the gskill-managed, reproducible artifacts kept out
// of version control: the content store/cache/locks (.gskill/) and the active
// skill layer (.agents/). The committed manifest + lockfile regenerate both via
// `gskill sync` (FR-007, clarification: gitignore both, regen on sync).
var gskillIgnorePatterns = []string{".gskill/", ".agents/"}

// ensureGitignore appends any missing gskill ignore hints, returning whether it
// changed the file.
func ensureGitignore(root string) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	existing, err := os.ReadFile(path) //nolint:gosec // project-root .gitignore
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read .gitignore: %w", err)
	}

	content := string(existing)
	changed := false
	for _, pattern := range gskillIgnorePatterns {
		if lineContains(content, pattern) {
			continue
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += pattern + "\n"
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := fsutil.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// lineContains reports whether content has pattern as a whole trimmed line, so
// ".agents/" is not considered present merely because ".agents/skills" appears.
func lineContains(content, pattern string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == pattern {
			return true
		}
	}
	return false
}

// sortedKeys returns the map keys in sorted order for deterministic iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// modeOr returns the first non-empty install-mode preference, defaulting to
// "auto" (prefer symlink, fall back to copy) per FR-022/FR-023.
func modeOr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return installer.DefaultModePref
}

// scopeOr maps an optional scope string to an installer.Scope, default project.
func scopeOr(scope string) installer.Scope {
	if scope == string(installer.ScopeGlobal) {
		return installer.ScopeGlobal
	}
	return installer.ScopeProject
}
