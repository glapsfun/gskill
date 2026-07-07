package app

import (
	"context"
	"fmt"

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
