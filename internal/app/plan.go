package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
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

// PlanInstall derives the installation plan for the selected skills: per
// skill × agent destinations, merge-vs-fresh decisions, and conflicts. It is
// pure computation over the manifest, lockfile, and discovery result — it
// acquires no lock and writes nothing (SC-002 is structural: only ExecutePlan
// writes).
func (a *App) PlanInstall(ctx context.Context, req PlanRequest) (InstallPlan, error) {
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
		if lfLoaded, err := loadOrNewLock(p.lockPath); err == nil {
			lf = lfLoaded
		}
	} else {
		plan.InitProject = true
	}

	agents, err := a.targetAgents(ctx, req.Root, req.AgentIDs, m.Defaults.Agents)
	if err != nil {
		return InstallPlan{}, err
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

	addReq := AddRequest{
		Root: req.Root, Source: req.Source,
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
		Agents: req.AgentIDs, Force: req.Force, Scope: req.Scope, Mode: req.Mode,
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
			return InstallPlan{}, planErr
		}
		_, declared := m.Skills[s.ID]
		appendSkillActions(&plan, req, s, ap, declared, home, global)
	}
	return plan, nil
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
	scan, err := a.installerForScope(p, string(ireq.Scope)).DiscoverAll(ctx, ireq, discovery.Options{})
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
// (by ID, RepoPath as tie-break), failing closed when a selected skill does
// not exist at the picked revision.
func remapSelected(selected []discovery.DiscoveredSkill, scan discovery.Result, rev resolver.Revision) ([]discovery.DiscoveredSkill, error) {
	out := make([]discovery.DiscoveredSkill, 0, len(selected))
	for _, want := range selected {
		var match *discovery.DiscoveredSkill
		for i := range scan.Skills {
			s := &scan.Skills[i]
			if s.ID != want.ID {
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
				errs.ErrInvalidManifest, want.ID, revisionLabel(rev))
		}
		out = append(out, *match)
	}
	return out, nil
}

// revisionLabel names a revision for error messages.
func revisionLabel(rev resolver.Revision) string {
	switch {
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
// flagging foreign-destination overwrites as conflicts: a destination that
// already exists for a skill gskill does not track is surfaced before approval
// rather than as an install failure afterward (FR-016, US4). --force is the
// documented escape hatch and overrides the conflict; a symlink resolving into
// a gskill-managed root (store, state dir, active layer) is gskill's own
// content — e.g. a lost lockfile — not foreign (review finding).
func appendSkillActions(plan *InstallPlan, req PlanRequest, s discovery.DiscoveredSkill, ap addPlan, declared bool, home string, global bool) {
	for _, ag := range ap.activate {
		dir := ag.ProjectSkillDir(req.Root)
		if global {
			dir = ag.GlobalSkillDir(home)
		}
		dest := filepath.Join(dir, s.ID)

		if !ap.mergeInto && !declared && !req.Force {
			if _, statErr := os.Stat(dest); statErr == nil && !destIsManaged(req.Root, dest) {
				err := &ConflictError{Skill: s.ID, Kind: ConflictFileOverwrite, err: fmt.Errorf(
					"%w: destination %s already exists and is not managed by gskill; remove it, or re-run with --force to overwrite",
					errs.ErrInvalidManifest, dest)}
				plan.Conflicts = append(plan.Conflicts, PlanConflict{
					Skill: s.ID, Kind: ConflictFileOverwrite, Detail: err.Error(), Err: err,
				})
				continue
			}
		}

		plan.Actions = append(plan.Actions, PlannedAction{
			Skill:       s.ID,
			AgentID:     ag.ID(),
			Destination: dest,
			MergeInto:   ap.mergeInto,
			FileOps:     planFileOps(s.Dir, dest),
		})
	}
}

// destIsManaged reports whether dest is a symlink pointing into a
// gskill-managed root: the project state dir (.gskill), the active layer
// (.agents), or the global config/store directory.
func destIsManaged(root, dest string) bool {
	fi, err := os.Lstat(dest)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(dest)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dest), target)
	}
	target = filepath.Clean(target)

	managed := []string{
		filepath.Join(root, stateDirName),
		filepath.Join(root, ".agents"),
	}
	if cfgDir, err := config.Dir(); err == nil {
		managed = append(managed, cfgDir)
	}
	for _, r := range managed {
		if pathWithin(target, r) {
			return true
		}
	}
	return false
}

// pathWithin reports whether path is inside root (or is root itself).
func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// planFileOps enumerates the files an action will place under dest,
// classifying each as create or update by whether the target already exists.
// Enumeration is read-only over the already-materialized skill dir; errors
// degrade to an empty list (the preview then shows only the destination).
func planFileOps(skillDir, dest string) []PlannedFileOp {
	if skillDir == "" {
		return nil
	}
	var ops []PlannedFileOp
	_ = filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best-effort preview enumeration
		}
		rel, relErr := filepath.Rel(skillDir, path)
		if relErr != nil {
			return nil //nolint:nilerr // best-effort preview enumeration
		}
		target := filepath.Join(dest, rel)
		op := "create"
		if _, statErr := os.Stat(target); statErr == nil {
			op = "update"
		}
		ops = append(ops, PlannedFileOp{Path: target, Op: op})
		return nil
	})
	return ops
}
