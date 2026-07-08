package app

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
)

// This file holds the phased use-case API behind the guided onboarding flow
// (spec 011, contracts/app-phases.md): DiscoverSource → PlanInstall →
// ExecutePlan. App.Add is the linear composition of the same phases, so the
// guided and non-guided paths cannot drift. PlanInstall lives in plan.go.

// DiscoverRequest describes phase 1: resolve a source and discover its skills.
// It works without a project manifest, so the wizard runs on fresh directories
// too (FR-023).
type DiscoverRequest struct {
	Root    string
	Source  string
	Version string
	Ref     string
	Commit  string
	Scope   string
	Mode    string

	// Discovery filters (FR-012 of spec 006).
	MaxDepth int
	Include  []string
	Exclude  []string
}

// DiscoverResult carries the resolved source and its skill catalog. Scan is the
// full discovery result (needed by selection); Skills is the catalog scoped to
// the source's explicit in-repo path, in display order.
type DiscoverResult struct {
	Ref      source.Ref
	Revision resolver.Revision
	Scan     discovery.Result
	Skills   []discovery.DiscoveredSkill
	Warnings []string
}

// ProgressEvent reports one step of an ExecutePlan run for progress UIs.
type ProgressEvent struct {
	Skill string
	Agent string
	Stage string // "install" (fetch+verify+stage+activate) or "record"
}

// Version candidate kinds offered by the wizard's version step (US3).
const (
	VersionLatest  = "latest"
	VersionRelease = "release"
	VersionBranch  = "branch"
	VersionCommit  = "commit"
)

// VersionCandidate is one selectable version of a source.
type VersionCandidate struct {
	Kind     string // VersionLatest | VersionRelease | VersionBranch | VersionCommit
	Label    string // display text, e.g. "v1.4.0", "main", "latest → v1.4.0"
	Version  string // bare semver for releases, when parseable
	Ref      string // tag or branch name to request
	Commit   string // exact SHA for commit candidates
	Metadata string // optional annotation shown next to the label
}

// VersionList is the version step's data. Listing problems are never fatal:
// Degraded marks that browsing is unavailable and why (FR-012).
type VersionList struct {
	Candidates     []VersionCandidate
	Degraded       bool
	DegradedReason string
}

// AgentChoice is one row of the wizard's agent step (US2, FR-014).
type AgentChoice struct {
	ID          string
	DisplayName string
	Detected    bool
	Preselected bool
}

// DiscoverSource resolves the source to a revision and discovers every skill in
// it. It writes only to the content cache/store (same as `add --list`), never
// to the manifest, lockfile, or agent directories.
func (a *App) DiscoverSource(ctx context.Context, req DiscoverRequest) (DiscoverResult, error) {
	ref, err := source.Parse(req.Source)
	if err != nil {
		return DiscoverResult{}, err
	}
	ref = promoteLocalGit(ref)

	rev, warnings, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
	})
	if err != nil {
		return DiscoverResult{}, err
	}

	p := openProject(req.Root)
	mode := req.Mode
	if p.manifestExists() {
		if m, mErr := manifest.Load(p.manifestPath); mErr == nil {
			mode = modeOr(req.Mode, m.Defaults.InstallMode)
		}
	}
	ireq := a.installRequest(req.Root, ref, rev, nil, req.Scope, mode)
	inst := a.installerForScope(p, string(ireq.Scope))
	scan, err := inst.DiscoverAll(ctx, ireq, discovery.Options{
		MaxDepth: req.MaxDepth, Include: req.Include, Exclude: req.Exclude,
	})
	if err != nil {
		return DiscoverResult{}, err
	}
	return DiscoverResult{
		Ref:      ref,
		Revision: rev,
		Scan:     scan,
		Skills:   skillsInScope(scan, ref.Path),
		Warnings: warnings,
	}, nil
}

// ListVersions lists the selectable versions of a source for the wizard's
// version step: a synthetic "latest" first, then releases (semver descending),
// other tags, and branch heads. Listing problems never fail the flow: offline
// mode, network errors, and rate limits all return a Degraded listing with a
// reason while "latest" stays selectable and a typed exact ref is still
// accepted downstream (FR-012).
func (a *App) ListVersions(ctx context.Context, root, src string, offline bool) (VersionList, error) {
	ref, err := source.Parse(src)
	if err != nil {
		return VersionList{}, err
	}
	ref = promoteLocalGit(ref)
	_ = root // reserved for cache-backed offline listings

	latest := VersionCandidate{Kind: VersionLatest, Label: "latest"}
	if ref.Type != source.TypeGit {
		// A plain local directory has no browsable versions.
		return VersionList{Candidates: []VersionCandidate{latest}}, nil
	}
	if offline {
		return VersionList{
			Candidates:     []VersionCandidate{latest},
			Degraded:       true,
			DegradedReason: "offline mode: version browsing needs the remote",
		}, nil
	}

	versions, err := resolver.ListVersions(ctx, a.git, ref)
	if err != nil {
		// Deliberate degrade-not-fail: listing problems must never abort the
		// flow (FR-012); the reason is surfaced in the step instead.
		return VersionList{ //nolint:nilerr // degradation is the contract, not an error path
			Candidates:     []VersionCandidate{latest},
			Degraded:       true,
			DegradedReason: err.Error(),
		}, nil
	}

	candidates := make([]VersionCandidate, 0, len(versions)+1)
	for _, v := range versions {
		c := VersionCandidate{Label: v.Name, Ref: v.Name, Metadata: shortCommit(v.Commit)}
		switch v.Kind {
		case resolver.VersionKindRelease:
			c.Kind = VersionRelease
			c.Version = v.Name
		case resolver.VersionKindTag:
			c.Kind = VersionRelease
		case resolver.VersionKindBranch:
			c.Kind = VersionBranch
		}
		candidates = append(candidates, c)
	}
	if len(candidates) > 0 && candidates[0].Kind == VersionRelease {
		latest.Label = "latest → " + candidates[0].Label
	}
	return VersionList{Candidates: append([]VersionCandidate{latest}, candidates...)}, nil
}

// AgentChoices returns the wizard's agent step data: every registered agent,
// which ones are detected for this project, and — preselected — the exact set a
// non-guided add would target (explicit-free resolution: manifest defaults,
// then detection, then the default agent), per FR-014.
func (a *App) AgentChoices(ctx context.Context, root string) ([]AgentChoice, error) {
	var defaults []string
	p := openProject(root)
	if p.manifestExists() {
		if m, err := manifest.Load(p.manifestPath); err == nil {
			defaults = m.Defaults.Agents
		}
	}

	// One registry-wide detection pass serves both the "detected" markers and
	// the preselection (defaults → detected → the default agent), mirroring
	// targetAgents' explicit-free resolution without re-probing (review
	// finding: doubled detection I/O on the welcome path).
	detected, err := a.agents.Detect(ctx, root)
	if err != nil {
		return nil, err
	}
	detectedIDs := make(map[string]bool, len(detected))
	for _, ag := range detected {
		detectedIDs[ag.ID()] = true
	}

	pre := detected
	if len(defaults) > 0 {
		pre, err = a.agentsByID(defaults)
		if err != nil {
			return nil, errs.WithHint(err, "run 'gskill doctor' to list detected agents")
		}
	} else if len(pre) == 0 {
		if def, ok := a.agents.Get(agent.DefaultID); ok {
			pre = []agent.Agent{def}
		}
	}
	preselected := make(map[string]bool, len(pre))
	for _, ag := range pre {
		preselected[ag.ID()] = true
	}

	all := a.agents.All()
	choices := make([]AgentChoice, 0, len(all))
	for _, ag := range all {
		choices = append(choices, AgentChoice{
			ID:          ag.ID(),
			DisplayName: ag.DisplayName(),
			Detected:    detectedIDs[ag.ID()],
			Preselected: preselected[ag.ID()],
		})
	}
	return choices, nil
}

// QualifiesLocalAgentAdd reports whether an add request is a pure agent-add —
// adding agents to already-locked skills from the same source — which App.Add
// serves entirely from the lockfile and store with no resolver or network
// call. The guided wizard has no equivalent shortcut, so such requests should
// take the direct path (review finding: offline interactive agent-adds).
// The check is read-only.
func (a *App) QualifiesLocalAgentAdd(_ context.Context, root string, req AddRequest) bool {
	if len(req.Agents) == 0 || disqualifiesLocalAdd(req) {
		return false
	}
	p := openProject(root)
	if !p.manifestExists() {
		return false
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return false
	}
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return false
	}
	_, ok := localAgentAddTargets(m, lf, req)
	return ok
}

// SelectByFlags resolves explicit --skill/--all selectors against a discovered
// source, exactly as the non-guided add does, so a flag-preselected wizard
// session and a scripted add choose identically (FR-004).
func (a *App) SelectByFlags(disc DiscoverResult, selectors []string, all bool, path string) ([]discovery.DiscoveredSkill, error) {
	return a.resolveSelection(disc.Scan, AddRequest{Selectors: selectors, All: all, Path: path}, disc.Ref.Path)
}

// ExecutePlan performs a previously computed InstallPlan: optional project
// initialization (FR-023), then the staged, checksum-verified, rollback-on-
// failure install and the atomic manifest+lockfile commit — the exact pipeline
// non-guided adds use. It refuses a conflicted plan outright (defense in depth;
// the wizard's approval step already blocks on conflicts, FR-016/FR-017).
// progress, when non-nil, receives per-skill events.
func (a *App) ExecutePlan(ctx context.Context, plan InstallPlan, progress func(ProgressEvent)) (AddResult, error) {
	if len(plan.Conflicts) > 0 {
		c := plan.Conflicts[0]
		if c.Err != nil {
			return AddResult{}, c.Err
		}
		return AddResult{}, fmt.Errorf("%w: %s", errs.ErrInvalidManifest, c.Detail)
	}
	if len(plan.Selected) == 0 {
		return AddResult{}, fmt.Errorf("%w: no skill selected", errs.ErrUsage)
	}

	p := openProject(plan.Root)
	if plan.InitProject {
		if _, err := a.Init(ctx, plan.Root); err != nil {
			return AddResult{}, err
		}
	}
	if !p.manifestExists() {
		return AddResult{}, errNoManifest()
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return AddResult{}, err
	}
	agents, err := a.agentsByID(plan.AgentIDs)
	if err != nil {
		return AddResult{}, err
	}

	req := AddRequest{
		Root:    plan.Root,
		Source:  plan.Source,
		Version: plan.Version,
		Ref:     plan.RequestedRef,
		Commit:  plan.RequestedCommit,
		Agents:  plan.ExplicitAgents,
		Force:   plan.Force,
		Scope:   plan.Scope,
		Mode:    plan.Mode,
	}
	ireq := a.installRequest(plan.Root, plan.SourceRef, plan.Revision, agents, plan.Scope, modeOr(plan.Mode, m.Defaults.InstallMode))
	inst := a.installerForScope(p, string(ireq.Scope))
	return a.installSelected(ctx, p, m, req, plan.SourceRef, plan.Revision, ireq, inst, plan.Selected, plan.Warnings, progress)
}
