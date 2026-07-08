package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
)

// Plan-time conflict kinds (spec 011 data-model.md). Detection semantics are
// exactly planAdd's, so guided and non-guided adds fail on the same conflicts.
const (
	ConflictCrossSource   = "cross_source_collision"
	ConflictNoopReadd     = "noop_readd"
	ConflictFileOverwrite = "file_overwrite"
)

// ConflictError is a plan-time conflict as an error. It wraps the same
// errs-coded error the non-guided add path returns, so message text, exit code,
// and errors.Is behavior are identical in both flows.
type ConflictError struct {
	Skill string
	Kind  string
	err   error
}

// Error implements the error interface.
func (e *ConflictError) Error() string { return e.err.Error() }

// Unwrap returns the underlying coded error.
func (e *ConflictError) Unwrap() error { return e.err }

// PlanRequest describes phase 3: derive an installation plan from the wizard
// session (or flag-derived answers). Version/Ref/Commit are the *requested*
// pins recorded as manifest intent; the resolved revision rides in Discover.
type PlanRequest struct {
	Root     string
	Source   string
	Version  string
	Ref      string
	Commit   string
	Discover DiscoverResult
	Selected []discovery.DiscoveredSkill
	// AgentIDs is the explicit agent selection ([] = resolve via manifest
	// defaults, then detection, then the default agent — same as --agent).
	AgentIDs []string
	Scope    string
	Mode     string
	Force    bool

	// Discovery filters, mirrored from the original DiscoverRequest so a
	// version re-pin re-discovers under the SAME constraints the user set
	// (review finding: dropping them could remap onto an excluded skill).
	MaxDepth int
	Include  []string
	Exclude  []string
}

// PlannedFileOp is one file the install will create or update (US4).
type PlannedFileOp struct {
	Path string `json:"path"`
	Op   string `json:"op"` // "create" or "update"
}

// PlannedAction is one skill × agent placement the plan will perform.
type PlannedAction struct {
	Skill       string          `json:"skill"`
	AgentID     string          `json:"agent"`
	Destination string          `json:"destination"`
	MergeInto   bool            `json:"merge_into,omitempty"` // agent-add into an existing install
	FileOps     []PlannedFileOp `json:"file_ops,omitempty"`
}

// PlanConflict is one conflict the preview shows. A non-empty conflict list
// blocks approval (FR-016) and makes ExecutePlan refuse the plan.
type PlanConflict struct {
	Skill  string `json:"skill"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	Err    error  `json:"-"`
}

// InstallPlan is the read-only output of PlanInstall: exactly what ExecutePlan
// will do, rendered by the wizard's preview and by `add --dry-run` (FR-015,
// FR-024). Computing it writes nothing.
type InstallPlan struct {
	Root   string `json:"-"`
	Source string `json:"source"`

	// Requested pins (manifest intent).
	Version         string `json:"version,omitempty"`
	RequestedRef    string `json:"ref,omitempty"`
	RequestedCommit string `json:"commit,omitempty"`

	SourceRef source.Ref        `json:"-"`
	Revision  resolver.Revision `json:"resolved"`

	Scope string `json:"scope,omitempty"`
	Mode  string `json:"mode,omitempty"`
	Force bool   `json:"force,omitempty"`

	// InitProject marks that no manifest exists yet: ExecutePlan scaffolds the
	// project first, and the preview lists the manifest as created (FR-023).
	InitProject bool `json:"init_project,omitempty"`

	Selected []discovery.DiscoveredSkill `json:"-"`
	// ExplicitAgents is the raw agent selection (manifest intent); AgentIDs is
	// the resolved target set.
	ExplicitAgents []string `json:"-"`
	AgentIDs       []string `json:"agents"`

	Actions   []PlannedAction `json:"actions"`
	Conflicts []PlanConflict  `json:"conflicts,omitempty"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// revisionSatisfies reports whether an already-resolved revision matches the
// requested pin, so PlanInstall re-resolves only on a genuine change.
func revisionSatisfies(rev resolver.Revision, req PlanRequest) bool {
	switch {
	case req.Commit != "":
		return rev.Commit == req.Commit
	case req.Ref != "":
		return rev.Tag == req.Ref || rev.Branch == req.Ref
	default:
		// No pin, or a semver constraint the discovery resolution already
		// honored (Add passes the same constraint to DiscoverSource).
		return true
	}
}

// PlanLine kinds, in emission order.
const (
	PlanLineMeta     = "meta"     // source / version / agents header
	PlanLineInit     = "init"     // project scaffolding notice (FR-023)
	PlanLineAgent    = "agent"    // per-agent group header ("claude:")
	PlanLineAction   = "action"   // "skill → destination"
	PlanLineFileOp   = "fileop"   // "create|update path"
	PlanLineWarning  = "warning"  // resolution/install warnings
	PlanLineConflict = "conflict" // blocking conflict detail
)

// PlanLine is one renderable line of an InstallPlan. The wizard preview and
// `add --dry-run` both render from this single sequence, so the two surfaces
// describe the same plan by construction (FR-015/FR-024); renderers add their
// own styling and prefixes per kind.
type PlanLine struct {
	Kind string
	Text string
}

// Lines flattens the plan into renderable lines. versionLabel overrides the
// version text (the wizard prefers the user's chosen label); "" derives it
// from the resolved revision.
func (p InstallPlan) Lines(versionLabel string) []PlanLine {
	if versionLabel == "" {
		versionLabel = RevisionLabel(p.Revision)
	}
	lines := []PlanLine{
		{Kind: PlanLineMeta, Text: "Source:  " + p.Source},
		{Kind: PlanLineMeta, Text: "Version: " + versionLabel},
		{Kind: PlanLineMeta, Text: "Agents:  " + strings.Join(p.AgentIDs, ", ")},
	}
	if p.InitProject {
		lines = append(lines, PlanLine{Kind: PlanLineInit, Text: ManifestName + " will be created (new project)"})
	}

	byAgent := map[string][]PlannedAction{}
	for _, act := range p.Actions {
		byAgent[act.AgentID] = append(byAgent[act.AgentID], act)
	}
	agents := make([]string, 0, len(byAgent))
	for id := range byAgent {
		agents = append(agents, id)
	}
	sort.Strings(agents)
	for _, id := range agents {
		lines = append(lines, PlanLine{Kind: PlanLineAgent, Text: id + ":"})
		for _, act := range byAgent[id] {
			lines = append(lines, PlanLine{Kind: PlanLineAction, Text: act.Skill + " → " + act.Destination})
			for _, op := range act.FileOps {
				lines = append(lines, PlanLine{Kind: PlanLineFileOp, Text: op.Op + " " + op.Path})
			}
		}
	}
	for _, w := range p.Warnings {
		lines = append(lines, PlanLine{Kind: PlanLineWarning, Text: w})
	}
	for _, c := range p.Conflicts {
		lines = append(lines, PlanLine{Kind: PlanLineConflict, Text: c.Detail})
	}
	return lines
}

// PlanInstall derives the installation plan for the selected skills: per
// skill × agent destinations, merge-vs-fresh decisions, and conflicts. It is
// pure computation over the manifest, lockfile, and discovery result — it
// acquires no lock and writes nothing (SC-002 is structural: only ExecutePlan
// writes).
func (a *App) PlanInstall(ctx context.Context, req PlanRequest) (InstallPlan, error) {
	return a.planInstallResolved(ctx, req, nil)
}

// planInstallResolved is PlanInstall with optionally pre-resolved target
// agents, so callers that already ran agent resolution (App.Add's fail-fast
// check) do not pay for a second registry-wide detection pass (review
// finding). agents == nil resolves here.
func (a *App) planInstallResolved(ctx context.Context, req PlanRequest, agents []agent.Agent) (InstallPlan, error) {
	p := openProject(req.Root)

	plan := InstallPlan{
		Root:            req.Root,
		Source:          req.Source,
		Version:         req.Version,
		RequestedRef:    req.Ref,
		RequestedCommit: req.Commit,
		SourceRef:       req.Discover.Ref,
		Revision:        req.Discover.Revision,
		Scope:           req.Scope,
		Mode:            req.Mode,
		Force:           req.Force,
		Selected:        req.Selected,
		ExplicitAgents:  req.AgentIDs,
		Warnings:        req.Discover.Warnings,
	}

	// A version picked in the wizard AFTER discovery must re-pin the plan:
	// the discovery-time resolution reflects the default (latest), not the
	// user's later choice (FR-013). Requested pins the resolution already
	// satisfies are not re-resolved, so the Add composition costs no extra
	// network round-trip.
	if !revisionSatisfies(plan.Revision, req) {
		if err := a.rePinPlan(ctx, req, p, &plan); err != nil {
			return InstallPlan{}, err
		}
	}
	selected := plan.Selected

	m := manifest.New()
	lf := lockfile.New()
	if p.manifestExists() {
		loaded, err := manifest.Load(p.manifestPath)
		if err != nil {
			return InstallPlan{}, err
		}
		m = loaded
		lfLoaded, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			// A corrupt lockfile is drift, and drift is an error — planning
			// against an empty lock would silently flip conflicts into merges
			// (constitution II; review finding).
			return InstallPlan{}, fmt.Errorf("load lockfile: %w", lockErr)
		}
		lf = lfLoaded
	} else {
		plan.InitProject = true
	}

	if agents == nil {
		resolved, err := a.targetAgents(ctx, req.Root, req.AgentIDs, m.Defaults.Agents)
		if err != nil {
			return InstallPlan{}, err
		}
		agents = resolved
	}
	reqIDs := agentIDs(agents)
	plan.AgentIDs = reqIDs

	// Global destinations need a real home directory; planning against ""
	// would preview garbage paths and stat the wrong overwrite targets
	// (review finding: silent os.UserHomeDir discard).
	home, homeErr := os.UserHomeDir()
	global := scopeOr(req.Scope) == installer.ScopeGlobal
	if global && homeErr != nil {
		return InstallPlan{}, fmt.Errorf("resolve home directory for a global install: %w", homeErr)
	}

	if err := a.planSelectedActions(&plan, req, p, m, lf, selected, reqIDs, home, global); err != nil {
		return InstallPlan{}, err
	}
	return plan, nil
}

// planSelectedActions runs per-skill conflict detection and action planning
// over the selected skills, appending actions and conflicts to the plan.
func (a *App) planSelectedActions(plan *InstallPlan, req PlanRequest, p *project, m *manifest.Manifest, lf *lockfile.Lockfile, selected []discovery.DiscoveredSkill, reqIDs []string, home string, global bool) error {
	addReq := AddRequest{
		Root: req.Root, Source: req.Source,
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
		Agents: req.AgentIDs, Force: req.Force, Scope: req.Scope, Mode: req.Mode,
	}
	roots := a.managedRoots(p)
	if cfgDir, cfgErr := config.Dir(); cfgErr == nil {
		roots = append(roots, cfgDir)
	}
	for _, s := range selected {
		ap, planErr := a.planAdd(m, lf, s.ID, addReq, reqIDs)
		if planErr != nil {
			var ce *ConflictError
			if errors.As(planErr, &ce) {
				plan.Conflicts = append(plan.Conflicts, PlanConflict{
					Skill: ce.Skill, Kind: ce.Kind, Detail: planErr.Error(), Err: planErr,
				})
				continue
			}
			return planErr
		}
		_, declared := m.Skills[s.ID]
		priorHash := ""
		if locked, ok := lf.Skills[s.ID]; ok {
			priorHash = locked.Resolved.ContentHash
		}
		appendSkillActions(plan, req, s, ap, declared, home, global, roots, priorHash)
	}
	return nil
}

// rePinPlan resolves the requested pin and re-discovers at that revision — the
// approved preview must describe the content that will actually install, and a
// skill absent at the picked version fails here, before approval (review
// finding).
func (a *App) rePinPlan(ctx context.Context, req PlanRequest, p *project, plan *InstallPlan) error {
	rev, warnings, err := resolver.Resolve(ctx, a.git, req.Discover.Ref, resolver.Requested{
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
	})
	if err != nil {
		return err
	}
	plan.Revision = rev
	plan.Warnings = append(plan.Warnings, warnings...)

	ireq := a.installRequest(req.Root, req.Discover.Ref, rev, nil, req.Scope, req.Mode)
	scan, err := a.installerForScope(p, string(ireq.Scope)).DiscoverAll(ctx, ireq, discovery.Options{
		MaxDepth: req.MaxDepth, Include: req.Include, Exclude: req.Exclude,
	})
	if err != nil {
		return err
	}
	remapped, err := remapSelected(req.Selected, scan, rev)
	if err != nil {
		return err
	}
	plan.Selected = remapped
	return nil
}

// remapSelected re-matches an earlier selection against a re-discovered scan
// (by ID among valid skills, RepoPath as tie-break), failing closed when a
// selected skill does not exist — or is not installable — at the picked
// revision (an invalid duplicate must never silently replace the selection).
func remapSelected(selected []discovery.DiscoveredSkill, scan discovery.Result, rev resolver.Revision) ([]discovery.DiscoveredSkill, error) {
	out := make([]discovery.DiscoveredSkill, 0, len(selected))
	for _, want := range selected {
		var match *discovery.DiscoveredSkill
		for i := range scan.Skills {
			s := &scan.Skills[i]
			if s.ID != want.ID || !s.Valid {
				continue
			}
			if s.RepoPath == want.RepoPath {
				match = s
				break
			}
			if match == nil {
				match = s
			}
		}
		if match == nil {
			return nil, fmt.Errorf("%w: skill %q does not exist at the selected version %s",
				errs.ErrInvalidManifest, want.ID, RevisionLabel(rev))
		}
		out = append(out, *match)
	}
	return out, nil
}

// RevisionLabel names a resolved revision for display and error messages —
// the single label shared by the wizard preview, dry-run output, and plan
// errors, so the surfaces cannot disagree on the same revision.
func RevisionLabel(rev resolver.Revision) string {
	switch {
	case rev.Version != "":
		return rev.Version
	case rev.Tag != "":
		return rev.Tag
	case rev.Branch != "":
		return rev.Branch
	case rev.Commit != "":
		return shortCommit(rev.Commit)
	default:
		return "latest"
	}
}

// appendSkillActions adds one selected skill's per-agent actions to the plan,
// flagging foreign-destination overwrites as conflicts: content gskill does
// not own — per-agent destinations AND the shared active-layer entry — is
// surfaced before approval rather than as an install failure afterward
// (FR-016, US4). Ownership is the shared active.Owned predicate the installer
// enforces at execute time, so plan and execution cannot drift; --force is the
// documented escape hatch. A copy-mode install (real dir whose hash matches
// the incoming or previously locked content) is gskill's own, not foreign.
func appendSkillActions(plan *InstallPlan, req PlanRequest, s discovery.DiscoveredSkill, ap addPlan, declared bool, home string, global bool, roots []string, priorHash string) {
	guarded := !ap.mergeInto && !declared && !req.Force
	rels := skillFiles(s.Dir)
	acceptHashes := func() []string {
		hashes := []string{priorHash}
		if h, err := integrity.HashDir(s.Dir); err == nil {
			hashes = append(hashes, h.ContentHash)
		}
		return hashes
	}

	// The shared active entry is ensured before any agent target; a foreign
	// occupant there fails execution, so surface it pre-approval too.
	if guarded && !global {
		activeDest := active.Path(req.Root, s.ID)
		if _, statErr := os.Lstat(activeDest); statErr == nil && !active.Owned(activeDest, roots, acceptHashes()...) {
			plan.Conflicts = append(plan.Conflicts, overwriteConflict(s.ID, activeDest))
			return
		}
	}

	for _, ag := range ap.activate {
		dir := ag.ProjectSkillDir(req.Root)
		if global {
			dir = ag.GlobalSkillDir(home)
		}
		dest := filepath.Join(dir, s.ID)

		if guarded {
			if _, statErr := os.Lstat(dest); statErr == nil && !active.Owned(dest, roots, acceptHashes()...) {
				plan.Conflicts = append(plan.Conflicts, overwriteConflict(s.ID, dest))
				continue
			}
		}

		plan.Actions = append(plan.Actions, PlannedAction{
			Skill:       s.ID,
			AgentID:     ag.ID(),
			Destination: dest,
			MergeInto:   ap.mergeInto,
			FileOps:     classifyFileOps(rels, dest),
		})
	}
}

// overwriteConflict builds the blocking foreign-destination conflict.
func overwriteConflict(skill, dest string) PlanConflict {
	err := &ConflictError{Skill: skill, Kind: ConflictFileOverwrite, err: fmt.Errorf(
		"%w: destination %s already exists and is not managed by gskill; remove it, or re-run with --force to overwrite",
		errs.ErrInvalidManifest, dest)}
	return PlanConflict{Skill: skill, Kind: ConflictFileOverwrite, Detail: err.Error(), Err: err}
}

// planFileOps enumerates the files an action will place under dest,
// classifying each as create or update by whether the target already exists.
// Enumeration is read-only over the already-materialized skill dir; errors
// degrade to an empty list (the preview then shows only the destination).
// skillFiles walks the materialized skill dir once, returning relative file
// paths; errors degrade to an empty list (the preview then shows only the
// destination). The walk is hoisted out of the per-agent loop so a multi-agent
// plan enumerates the source tree once (review finding).
func skillFiles(skillDir string) []string {
	if skillDir == "" {
		return nil
	}
	var rels []string
	_ = filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best-effort preview enumeration
		}
		rel, relErr := filepath.Rel(skillDir, path)
		if relErr != nil {
			return nil //nolint:nilerr // best-effort preview enumeration
		}
		rels = append(rels, rel)
		return nil
	})
	return rels
}

// classifyFileOps maps the skill's relative file list onto one destination,
// classifying each file as create or update by whether the target exists.
func classifyFileOps(rels []string, dest string) []PlannedFileOp {
	ops := make([]PlannedFileOp, 0, len(rels))
	for _, rel := range rels {
		target := filepath.Join(dest, rel)
		op := "create"
		if _, statErr := os.Stat(target); statErr == nil {
			op = "update"
		}
		ops = append(ops, PlannedFileOp{Path: target, Op: op})
	}
	return ops
}
