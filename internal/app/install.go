package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/integrity"

	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/selection"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/source"
)

const defaultLockTimeout = 30 * time.Second

// InitResult reports what Init created.
type InitResult struct {
	LockPath string
	Created  []string
}

// Init prepares local gskill runtime state: the .gskill state dir, the
// canonical .agents/skills layer, and .gitignore hints. It never creates a
// manifest; an empty skills-lock.json is written only when withLock is set.
// It is idempotent.
func (a *App) Init(_ context.Context, root string, withLock bool) (InitResult, error) {
	p, err := a.openProjectScoped(root)
	if err != nil {
		return InitResult{}, err
	}
	res := InitResult{LockPath: p.lockPath}

	for _, dir := range []string{filepath.Join(root, stateDirName), active.Dir(root)} {
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil {
			rel = dir
		}
		if !fileExists(dir) {
			res.Created = append(res.Created, rel)
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return InitResult{}, fmt.Errorf("create %s: %w", rel, err)
		}
	}
	added, err := ensureGitignore(root)
	if err != nil {
		return InitResult{}, err
	}
	if added {
		res.Created = append(res.Created, ".gitignore")
	}
	if withLock && !fileExists(p.lockPath) {
		if err := skillslock.Save(p.lockPath, skillslock.New()); err != nil {
			return InitResult{}, err
		}
		res.Created = append(res.Created, skillslock.FileName)
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

	// Progress, when non-nil, receives install lifecycle events (spec 014,
	// contracts/install-progress-events.md); nil keeps the run unobserved.
	Progress func(InstallProgressEvent)
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

// Add resolves, installs, and records a new skill, updating the shared lock.
// It errors on an already-declared key unless Force is set (FR-047), and
// writes nothing when no target agent is available (FR-029).
func (a *App) Add(ctx context.Context, req AddRequest) (AddResult, error) {
	p, err := a.openProjectScoped(req.Root)
	if err != nil {
		return AddResult{}, err
	}

	// Adding an agent to an already-installed skill is a local relink: reuse the
	// locked revision and existing store, with no resolve or network (FR-001).
	if res, handled, laErr := a.tryLocalAgentAdd(ctx, p, req); handled {
		return res, laErr
	}

	// Phase 1: resolve + discover (read-only outside the cache/store).
	disc, err := a.DiscoverSource(ctx, DiscoverRequest{
		Root: req.Root, Source: req.Source,
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
		Scope: req.Scope, Mode: req.Mode,
		MaxDepth: req.MaxDepth, Include: req.Include, Exclude: req.Exclude,
	})
	if err != nil {
		return AddResult{}, err
	}

	if req.ListOnly {
		return AddResult{Listed: disc.Skills, Warnings: disc.Warnings}, nil
	}

	// An install needs a target agent. Resolve it before any interactive
	// selection so the user is not asked to pick skills only to fail afterward;
	// the resolved set is threaded into the plan so detection runs once.
	agents, err := a.targetAgents(ctx, req.Root, req.Agents, nil)
	if err != nil {
		return AddResult{}, err
	}

	selected, err := a.resolveSelection(disc.Scan, req, disc.Ref.Path)
	if err != nil {
		return AddResult{}, err
	}

	// Phases 3+4: plan (pure) then execute. Add is the linear composition of
	// the wizard's phases, so guided and non-guided installs cannot drift
	// (spec 011 SC-004, constitution I).
	plan, err := a.planInstallResolved(ctx, PlanRequest{
		Root: req.Root, Source: req.Source,
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
		Discover: disc, Selected: selected,
		AgentIDs: req.Agents, Scope: req.Scope, Mode: req.Mode, Force: req.Force,
		MaxDepth: req.MaxDepth, Include: req.Include, Exclude: req.Exclude,
	}, agents)
	if err != nil {
		return AddResult{}, err
	}
	return a.ExecutePlan(ctx, plan, req.Progress)
}

// tryLocalAgentAdd handles a pure agent-add — adding agents to one or more
// already-locked skills from the same source — entirely from the lockfile and
// store, with no resolver or network call (FR-001, review F7). It returns
// handled=true when it took ownership of the request (success or a conflict
// error); handled=false means the caller should fall through to the normal
// resolve+install path.
func (a *App) tryLocalAgentAdd(ctx context.Context, p *project, req AddRequest) (AddResult, bool, error) {
	if disqualifiesLocalAdd(req) {
		return AddResult{}, false, nil
	}
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return AddResult{}, false, nil //nolint:nilerr // fall back to the normal path on a lock read error
	}
	targets, ok := localAgentAddTargets(lf, req)
	if !ok {
		return AddResult{}, false, nil
	}

	agents, err := a.targetAgents(ctx, req.Root, req.Agents, nil)
	if err != nil {
		return AddResult{}, true, err
	}
	reqIDs := agentIDs(agents)
	if !anyNewAgent(lf, targets, reqIDs) {
		return AddResult{}, true, conflictErr(targets[0])
	}

	res, err := a.materializeLocalAgentAdd(ctx, p, targets, reqIDs)
	return res, true, err
}

// disqualifiesLocalAdd reports whether a request can't be a pure agent-add: it
// changes the pin, only lists, selects everything, or asks for a placement the
// locked-scope relink cannot honor — an explicit path, a global scope, or an
// install mode (the fast path reuses the locked scope and mode verbatim, so
// honoring those flags requires the full plan/install pipeline; review
// finding: --global/--copy/--path were silently discarded).
func disqualifiesLocalAdd(req AddRequest) bool {
	return req.Force || req.ListOnly || req.All ||
		req.Version != "" || req.Ref != "" || req.Commit != "" ||
		req.Path != "" || req.Mode != "" ||
		req.Scope == string(installer.ScopeGlobal)
}

// anyNewAgent reports whether any target skill gains a not-yet-installed agent.
func anyNewAgent(lf *skillslock.State, targets, reqIDs []string) bool {
	for _, name := range targets {
		if len(Subtract(reqIDs, lf.Skills[name].Installation.Agents)) > 0 {
			return true
		}
	}
	return false
}

// materializeLocalAgentAdd relinks the new agents for every target skill under
// the project lock, with no resolution.
func (a *App) materializeLocalAgentAdd(ctx context.Context, p *project, targets, reqIDs []string) (AddResult, error) {
	res := AddResult{}
	err := a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		for _, name := range targets {
			if relErr := a.relinkAgents(ctx, p, lf, name, reqIDs, &res); relErr != nil {
				return relErr
			}
		}
		return saveLock(p.lockPath, lf)
	})
	return res, err
}

// relinkAgents activates the not-yet-installed agents for one locked skill from
// the lock (no resolve) and merges the result into the lock entry.
func (a *App) relinkAgents(ctx context.Context, p *project, lf *skillslock.State, name string, reqIDs []string, res *AddResult) error {
	locked := lf.Skills[name]
	newIDs := Subtract(reqIDs, locked.Installation.Agents)
	if len(newIDs) == 0 {
		return nil
	}
	newAgents, err := a.agentsByID(newIDs)
	if err != nil {
		return err
	}
	result, err := a.reconcileFromLock(ctx, p, name, locked, newAgents, SyncRequest{Root: p.root}, true)
	if err != nil {
		return err
	}
	mergeAgentInstall(&locked, result)

	// Backfill the pin from the locked revision (no re-resolve) so an
	// agent-add to an unpinned skill still records tracking intent.
	locked.Requested = backfillRequested(locked.Requested, revFromLock(locked.Resolved))
	lf.Skills[name] = locked

	res.Installed = append(res.Installed, InstalledSkill{
		Name: name, Path: locked.Source.Path, ContentHash: locked.Resolved.ContentHash, Targets: result.Targets,
	})
	return nil
}

// localAgentAddTargets returns the already-locked skill names a pure agent-add
// targets, or ok=false when the request is not a pure agent-add (globby/`*`/path
// selectors, a source that is not uniquely locked, or any target whose source
// differs from the request).
func localAgentAddTargets(lf *skillslock.State, req AddRequest) ([]string, bool) {
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
		for name, locked := range lf.Skills {
			if locked.Source.Original == req.Source {
				cands = append(cands, name)
			}
		}
		if len(cands) != 1 {
			return nil, false
		}
		names = cands
	}

	for _, name := range names {
		locked, inL := lf.Skills[name]
		if !inL || locked.Source.Original != req.Source {
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
			errs.ErrUsage, len(valid),
		)
	case len(invalid) > 0:
		return nil, fmt.Errorf("%w: skill %q is invalid: %s",
			errs.ErrInvalidLock, invalid[0].ID, firstProblem(invalid[0]))
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
// any failure, and commits the lock only after all succeed.
// progress, when non-nil, receives install lifecycle events (spec 014): a
// running storing event before each skill's placement and one terminal
// installed event after it records, plus a run-scoped locking event before
// the lock write.
func (a *App) installSelected(ctx context.Context, p *project, req AddRequest, ref source.Ref, rev resolver.Revision, ireq installer.Request, inst *installer.Installer, selected []discovery.DiscoveredSkill, warnings []string, progress func(InstallProgressEvent)) (AddResult, error) {
	emit := func(index int, skill string, phase InstallPhase, status InstallStatus) {
		if progress == nil {
			return
		}
		progress(InstallProgressEvent{
			SkillIndex: index, SkillTotal: len(selected), SkillName: skill,
			Source: ref.Original, SourceType: string(ref.Type),
			Version: rev.Version, Ref: rev.Tag, Commit: rev.Commit,
			Phase: phase, Status: status,
		})
	}
	res := AddResult{Warnings: append([]string(nil), warnings...)}
	reqIDs := agentIDs(ireq.Agents)
	err := a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		external, extErr := declaredExternalNames(p.lockPath, lf)
		if extErr != nil {
			return extErr
		}
		var activated []installer.Result
		rollback := func() {
			for _, r := range activated {
				a.removeTargets(req.Root, ireq.Scope, r)
			}
		}

		for k, s := range selected {
			// Honor interruption between skills: an interrupted multi-skill add
			// rolls back what it activated instead of committing a partial set
			// (spec 011 FR-020, SC-006 interrupt class). The cancellation is
			// wrapped in ErrCancelled so the CLI reports it plainly with exit
			// 130 (spec 014 FR-025) instead of a generic error.
			if ctxErr := ctx.Err(); ctxErr != nil {
				rollback()
				return fmt.Errorf("%w: %w", errs.ErrCancelled, ctxErr)
			}
			emit(k+1, s.ID, InstallPhaseStoring, InstallStatusRunning)
			plan, planErr := a.planAdd(lf, external, s.ID, req, reqIDs)
			if planErr != nil {
				rollback()
				return planErr
			}
			ir := ireq
			ir.Name = s.ID
			ir.Path = s.RepoPath
			ir.Agents = plan.activate
			// Adds never clobber content gskill does not own; --force is the
			// documented override (spec 011 FR-016). The previously locked
			// hash marks copy-mode installs as gskill's own.
			ir.PreserveForeign = !req.Force
			if locked, ok := lf.Skills[s.ID]; ok {
				ir.PriorContentHash = locked.Resolved.ContentHash
			}
			result, instErr := inst.Install(ctx, ir)
			if instErr != nil {
				rollback()
				return instErr
			}
			activated = append(activated, result)
			if lockErr := recordAdd(lf, req, s, ref, rev, ir, result, plan); lockErr != nil {
				rollback()
				return lockErr
			}
			res.Installed = append(res.Installed, InstalledSkill{
				Name: s.ID, Path: s.RepoPath, ContentHash: result.ContentHash, Targets: result.Targets,
			})
			res.Warnings = append(res.Warnings, result.Warnings...)
			emit(k+1, s.ID, InstallPhaseComplete, InstallStatusInstalled)
		}

		emitRunPhase(progress, InstallPhaseLocking, len(selected))
		if saveErr := saveLock(p.lockPath, lf); saveErr != nil {
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

// addPlan is the agent set to activate now for one selected skill plus the
// agent IDs to persist in the lock entry. mergeInto marks an agent-add (the
// skill is already installed), so only the new agents are activated and the
// result is merged into the existing lock entry rather than replacing it.
type addPlan struct {
	activate   []agent.Agent // agents to activate now (only the new ones for an agent-add)
	persistIDs []string      // agent IDs to record in the lock entry
	mergeInto  bool
}

// planAdd decides how a selected skill installs given the existing lock state,
// failing closed on a genuine conflict or cross-source collision. external
// holds the shared lock's external-only entry names: another tool's entry is
// never hijacked by an add (a merged rewrite would keep its core source while
// replacing its hash — a corrupted entry), so the collision always fails.
func (a *App) planAdd(lf *skillslock.State, external map[string]bool, id string, req AddRequest, reqIDs []string) (addPlan, error) {
	existing, exists := lf.Skills[id]
	if !exists && external[id] {
		return addPlan{}, &ConflictError{Skill: id, Kind: ConflictCrossSource, err: fmt.Errorf(
			"%w: skill %q is already declared in %s by another tool; run 'gskill install' to adopt that entry, or use a different name",
			errs.ErrInvalidLock, id, skillslock.FileName,
		)}
	}
	if !exists || req.Force {
		ags, err := a.agentsByID(reqIDs)
		return addPlan{activate: ags, persistIDs: reqIDs}, err
	}

	if existing.Source.Original != req.Source {
		return addPlan{}, &ConflictError{Skill: id, Kind: ConflictCrossSource, err: fmt.Errorf(
			"%w: skill %q is already declared from a different source %q (name collision); use a different name or 'gskill remove %s' first",
			errs.ErrInvalidLock, id, existing.Source.Original, id,
		)}
	}

	current := existing.Installation.Agents
	newOnes := Subtract(reqIDs, current)
	if len(newOnes) == 0 {
		return addPlan{}, conflictErr(id)
	}

	// Activate only the new agents; the existing targets are untouched and the
	// result is merged into the lock entry (FR-001..FR-005).
	ags, err := a.agentsByID(newOnes)
	return addPlan{activate: ags, persistIDs: unionStrings(current, reqIDs), mergeInto: true}, err
}

// conflictErr reports an already-declared skill with no new agent to add, as a
// typed plan conflict so PlanInstall can classify it (spec 011).
func conflictErr(id string) error {
	return &ConflictError{Skill: id, Kind: ConflictNoopReadd, err: fmt.Errorf(
		"%w: skill %q already declared; use 'gskill update %s' or --force",
		errs.ErrInvalidLock, id, id,
	)}
}

// mergeAgentInstall folds an agent-add install result into an existing lock
// entry, unioning agents and merging the per-agent target/mode records while
// preserving the resolved revision, source, and active path.
func mergeAgentInstall(locked *skillslock.Record, result installer.Result) {
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
		return errs.Wrap(errs.CodeInvalidLock, err.Error(), err)
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

// firstProblem returns the first error-severity diagnostic message for a
// skill, with a fallback for error paths that need a non-empty reason.
func firstProblem(s discovery.DiscoveredSkill) string {
	if msg := s.FirstError(); msg != "" {
		return msg
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
}

// InstallResult reports an install run.
type InstallResult struct {
	Skills  []SkillChange
	Changed bool
}

// stampSkill marks every progress event from one skill's resolve/fetch with
// the skill's name and its [k/N] position, so the CLI can render a multi-repo
// counter and per-skill completion lines.
func stampSkill(ctx context.Context, name string, index, count int) context.Context {
	return progress.Stamp(ctx, func(e *progress.Event) {
		e.Skill, e.Index, e.Count = name, index, count
	})
}

// skillIntent is the desired-state input to one skill's install: what the
// user asked for (add flags) or what the lock's gskill block declares
// (update/restore). It replaces the manifest declaration.
type skillIntent struct {
	Source  string
	Path    string
	Version string
	Ref     string
	Commit  string
	Mode    string
	Scope   string
	Agents  []string
}

// intentFromRecord derives the declared intent from a managed lock record.
func intentFromRecord(r skillslock.Record) skillIntent {
	return skillIntent{
		Source:  r.Source.Original,
		Path:    r.Source.Path,
		Version: r.Requested.Version,
		Ref:     r.Requested.Ref,
		Commit:  r.Requested.Commit,
		Mode:    r.Installation.Mode,
		Scope:   r.Installation.Scope,
		Agents:  r.Installation.Agents,
	}
}

// installOne installs a single declared skill and updates lf in place.
func (a *App) installOne(ctx context.Context, p *project, lf *skillslock.State, name string, in skillIntent, req InstallRequest) (SkillChange, error) {
	ref, err := source.Parse(in.Source)
	if err != nil {
		return SkillChange{}, err
	}
	ref = promoteLocalGit(ref)
	agents, err := a.targetAgents(ctx, p.root, in.Agents, nil)
	if err != nil {
		return SkillChange{}, err
	}
	rev, _, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{
		Version: in.Version, Ref: in.Ref, Commit: in.Commit,
	})
	if err != nil {
		return SkillChange{}, err
	}

	// Backfill the tracking intent before building the lock entry, so the
	// record's `requested` stays satisfied on the next run.
	rq := backfillRequested(
		skillslock.Requested{Version: in.Version, Ref: in.Ref, Commit: in.Commit}, rev,
	)

	ireq := a.installRequest(p.root, ref, rev, agents, cmp.Or(in.Scope, req.Scope), modeOr(req.Mode, in.Mode))
	ireq.Name = name
	ireq.Offline = req.Offline
	// Honor the declared in-repo path so a multi-skill source resolves to the
	// declared skill instead of erroring on multiple SKILL.md files.
	if in.Path != "" {
		ireq.Path = in.Path
	}
	result, err := a.installerForScope(p, string(ireq.Scope)).Install(ctx, ireq)
	if err != nil {
		return SkillChange{}, err
	}

	old, existed := lf.Skills[name]
	changed := !existed || old.Resolved.ContentHash != result.ContentHash || old.Resolved.Commit != rev.Commit
	locked, lockErr := buildLockEntry(ref, rev, ireq, result, rq)
	if lockErr != nil {
		return SkillChange{}, lockErr
	}
	lf.Skills[name] = locked
	return SkillChange{Name: name, ContentHash: result.ContentHash, Changed: changed}, nil
}

// agentsByID maps agent IDs to registered agents, failing on unknown IDs.
func (a *App) agentsByID(ids []string) ([]agent.Agent, error) {
	out := make([]agent.Agent, 0, len(ids))
	for _, id := range ids {
		ag, ok := a.agents.Get(id)
		if !ok {
			return nil, errs.WithHint(
				fmt.Errorf("%w: locked agent %q is not available", errs.ErrUnsupportedAgent, id),
				"run 'gskill doctor' to list detected agents",
			)
		}
		out = append(out, ag)
	}
	return out, nil
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
					"run 'gskill doctor' to list detected agents",
				)
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
		"pass --agent <id>, or run 'gskill doctor' to see why detection found nothing",
	)
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

// backfillRequested fills an empty tracking intent from the resolved revision,
// mapping by ref-kind: semver→caret range (kept floating so updates are not
// frozen), tag/branch→ref, commit→commit; a local source has no resolvable
// version and stays unpinned. Explicit values are never overwritten.
func backfillRequested(rq skillslock.Requested, rev resolver.Revision) skillslock.Requested {
	if rq.Version != "" || rq.Ref != "" || rq.Commit != "" {
		return rq
	}
	switch rev.RefKind {
	case resolver.RefKindSemver:
		rq.Version = "^" + rev.Version
	case resolver.RefKindTag:
		rq.Ref = rev.Tag
	case resolver.RefKindBranch:
		rq.Ref = rev.Branch
	case resolver.RefKindCommit:
		rq.Commit = rev.Commit
	case resolver.RefKindLocal:
		// No resolvable version; leave the intent unpinned.
	}
	return rq
}

// recordAdd writes the lock entry for one added skill. An agent-add
// (plan.mergeInto) merges the result into the existing lock entry, backfilling
// the pin from the already-locked revision (not the freshly-resolved rev, so
// the pin matches what is locked). A fresh add builds a new lock entry with
// the resolved pin backfilled so restore stays idempotent.
func recordAdd(lf *skillslock.State, req AddRequest, s discovery.DiscoveredSkill, ref source.Ref, rev resolver.Revision, ireq installer.Request, result installer.Result, plan addPlan) error {
	if plan.mergeInto {
		locked := lf.Skills[s.ID]
		mergeAgentInstall(&locked, result)
		locked.Installation.Agents = unionStrings(locked.Installation.Agents, plan.persistIDs)
		locked.Requested = backfillRequested(locked.Requested, revFromLock(locked.Resolved))
		lf.Skills[s.ID] = locked
		return nil
	}
	rq := backfillRequested(
		skillslock.Requested{Version: req.Version, Ref: req.Ref, Commit: req.Commit}, rev,
	)
	locked, err := buildLockEntry(ref, rev, ireq, result, rq)
	if err != nil {
		return err
	}
	lf.Skills[s.ID] = locked
	return nil
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

// Subtract returns the values in a that are not in b.
func Subtract(a, b []string) []string {
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

// buildLockEntry assembles the lock record from resolution + install reality.
func buildLockEntry(ref source.Ref, rev resolver.Revision, ireq installer.Request, result installer.Result, rq skillslock.Requested) (skillslock.Record, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	fm := result.Skill.Frontmatter
	resolved := skillslock.Resolved{
		Version:       rev.Version,
		RefKind:       string(rev.RefKind),
		Tag:           rev.Tag,
		Branch:        rev.Branch,
		Commit:        rev.Commit,
		ContentHash:   result.ContentHash,
		SkillFileHash: result.SkillFileHash,
		MutableRef:    rev.MutableRef,
	}
	// Record the shared computedHash so the skills-lock.json entry stays
	// consumable by external tooling (spec 012 FR-024). A hashing failure must
	// surface: silently leaving it empty would let a stale computedHash for
	// changed content survive in the shared lock.
	compat, err := integrity.CompatHash(result.Skill.Dir)
	if err != nil {
		return skillslock.Record{}, fmt.Errorf("compute shared computedHash for %s: %w", ireq.Name, err)
	}
	resolved.CompatHash = compat
	if rev.RefKind == resolver.RefKindLocal {
		resolved.LocalPathHash = result.ContentHash
	}
	return skillslock.Record{
		Source: skillslock.Source{
			Type: string(ref.Type), Original: ref.Original, URL: ref.URL,
			Owner: ref.Owner, Repo: ref.Repo, Path: ireq.Path,
		},
		Requested: rq,
		Resolved:  resolved,
		Metadata: skillslock.Metadata{
			Name: fm.Name, Description: fm.Description, Version: fm.Version, License: fm.License,
		},
		Requires: skillslock.Requires{
			Skills: fm.Requires.Skills, Commands: fm.Requires.Commands,
			Environment: fm.Requires.Environment, MCP: fm.Requires.MCP,
		},
		Installation: skillslock.Installation{
			Scope: string(ireq.Scope), Mode: string(result.Mode),
			Agents: result.Agents, ActivePath: result.ActivePath,
			Targets: result.Targets, Modes: result.Modes,
		},
		Provenance: skillslock.Provenance{FetchedAt: now, UpdatedAt: now, Trust: "checksum-ok"},
	}, nil
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
func refFromLock(src skillslock.Source) source.Ref {
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
func revFromLock(res skillslock.Resolved) resolver.Revision {
	return resolver.Revision{
		RefKind:    resolver.RefKind(res.RefKind),
		Version:    res.Version,
		Tag:        res.Tag,
		Branch:     res.Branch,
		Commit:     res.Commit,
		MutableRef: res.MutableRef,
	}
}

// loadOrNewLock loads the shared skills-lock.json at path into the in-memory
// state (only entries carrying a gskill block surface; external-only entries
// stay on disk untouched), or returns a fresh state if absent.
func loadOrNewLock(path string) (*skillslock.State, error) {
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat lockfile: %w", err)
		}
		return skillslock.NewState(), nil
	}
	l, err := skillslock.Load(path)
	if err != nil {
		return nil, err
	}
	// A corrupt gskill block must fail closed here: silently treating the
	// entry as external-only would drop it from restore/remove/sync.
	if err := l.CheckExts(); err != nil {
		return nil, err
	}
	lf := skillslock.NewState()
	for _, name := range l.Names() {
		e, _ := l.Entry(name)
		if e.Ext == nil {
			continue
		}
		lf.Skills[name] = skillslock.ToRecord(name, e)
	}
	return lf, nil
}

// saveLock writes the in-memory state through the lossless shared lock:
// managed entries are synced (never clearing a recorded computedHash), entries
// gskill removed are dropped, and every field gskill does not understand —
// unknown keys, external-only entries, other tools' blocks — is preserved
// byte-for-byte (FR-003, FR-006).
func saveLock(path string, lf *skillslock.State) error {
	var l *skillslock.Lock
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat lockfile: %w", err)
		}
		l = skillslock.New()
	} else {
		var lerr error
		l, lerr = skillslock.Load(path)
		if lerr != nil {
			// Fail closed: never clobber a shared file gskill cannot parse.
			return lerr
		}
	}
	for _, name := range l.Names() {
		e, _ := l.Entry(name)
		if e.Ext == nil {
			continue
		}
		if _, ok := lf.Skills[name]; !ok {
			l.Remove(name)
		}
	}
	for _, name := range sortedKeys(lf.Skills) {
		l.SetEntry(name, skillslock.FromRecord(lf.Skills[name]))
	}
	return skillslock.Save(path, l)
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
