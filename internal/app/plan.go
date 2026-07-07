package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/discovery"
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

	addReq := AddRequest{
		Root: req.Root, Source: req.Source,
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
		Agents: req.AgentIDs, Force: req.Force, Scope: req.Scope, Mode: req.Mode,
	}
	home, _ := os.UserHomeDir()
	global := scopeOr(req.Scope) == installer.ScopeGlobal

	for _, s := range req.Selected {
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
		for _, ag := range ap.activate {
			dir := ag.ProjectSkillDir(req.Root)
			if global {
				dir = ag.GlobalSkillDir(home)
			}
			plan.Actions = append(plan.Actions, PlannedAction{
				Skill:       s.ID,
				AgentID:     ag.ID(),
				Destination: filepath.Join(dir, s.ID),
				MergeInto:   ap.mergeInto,
			})
		}
	}
	return plan, nil
}
