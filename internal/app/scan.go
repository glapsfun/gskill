package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/store"
)

// SkillInspection is the detailed view of one discovered skill (FR-033).
type SkillInspection struct {
	Skill  discovery.DiscoveredSkill
	Source string   // source identity (host/owner/repo or local path)
	Agents []string // agents the skill would install into at the given root
}

// SourceCheckReport summarizes a source's defects (FR-034).
type SourceCheckReport struct {
	Invalid    []discovery.DiscoveredSkill
	Duplicates []discovery.DuplicateConflict
}

// HasProblems reports whether the source has any invalid or duplicate skills.
func (r SourceCheckReport) HasProblems() bool {
	return len(r.Invalid) > 0 || len(r.Duplicates) > 0
}

// ScanOptions configures a read-only source scan.
type ScanOptions struct {
	Ref      string
	MaxDepth int
	Include  []string
	Exclude  []string
}

// SourceList scans a source and returns every discovered skill, read-only
// (FR-032). It never writes to a manifest, lockfile, or agent directory.
func (a *App) SourceList(ctx context.Context, sourceArg string, opts ScanOptions) (discovery.Result, error) {
	_, res, err := a.scanSource(ctx, sourceArg, opts)
	return res, err
}

// SourceInspect scans a source and returns the detailed view of one selected
// skill (FR-033). The selector is a name or name@path; invalid skills can be
// inspected so their diagnostics are visible.
func (a *App) SourceInspect(ctx context.Context, sourceArg, selector, root string, opts ScanOptions) (SkillInspection, error) {
	ref, res, err := a.scanSource(ctx, sourceArg, opts)
	if err != nil {
		return SkillInspection{}, err
	}
	skill, found := findSkill(res, selector)
	if !found {
		return SkillInspection{}, fmt.Errorf("%w: no skill matching %q in source", errs.ErrUsage, selector)
	}
	agents, _ := a.agents.Detect(ctx, root)
	ids := make([]string, 0, len(agents))
	for _, ag := range agents {
		ids = append(ids, ag.ID())
	}
	return SkillInspection{Skill: skill, Source: ref.Identity(), Agents: ids}, nil
}

// SourceCheck scans a source and reports its invalid and duplicate skills
// (FR-034). It is read-only; the caller maps HasProblems to a non-zero exit.
func (a *App) SourceCheck(ctx context.Context, sourceArg string, opts ScanOptions) (SourceCheckReport, error) {
	_, res, err := a.scanSource(ctx, sourceArg, opts)
	if err != nil {
		return SourceCheckReport{}, err
	}
	report := SourceCheckReport{Duplicates: res.Duplicates}
	for _, s := range res.Skills {
		if !s.Valid {
			report.Invalid = append(report.Invalid, s)
		}
	}
	return report, nil
}

// scanSource resolves, materializes, and recursively scans a source read-only.
func (a *App) scanSource(ctx context.Context, sourceArg string, opts ScanOptions) (source.Ref, discovery.Result, error) {
	ref, err := source.Parse(sourceArg)
	if err != nil {
		return source.Ref{}, discovery.Result{}, err
	}
	ref = promoteLocalGit(ref)

	rev, _, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{Ref: opts.Ref})
	if err != nil {
		return source.Ref{}, discovery.Result{}, err
	}

	ireq := installer.Request{Ref: ref, Revision: rev, Path: ref.Path}
	res, err := a.scanInstaller().DiscoverAll(ctx, ireq, discovery.Options{
		MaxDepth: opts.MaxDepth, Include: opts.Include, Exclude: opts.Exclude,
	})
	if err != nil {
		return source.Ref{}, discovery.Result{}, err
	}
	return ref, res, nil
}

// scanInstaller builds a read-only installer backed by the global cache/store,
// used only to materialize sources for discovery (no activation occurs).
func (a *App) scanInstaller() *installer.Installer {
	cfgDir, err1 := config.Dir()
	cacheDir, err2 := config.CacheDir()
	if err1 != nil || err2 != nil {
		return installer.New(a.git, cache.New(filepath.Join(stateDirName, "cache")), store.New(filepath.Join(stateDirName, "store")))
	}
	return installer.New(a.git, cache.New(cacheDir), store.New(filepath.Join(cfgDir, "store")))
}

// findSkill locates a discovered skill by a name or name@path selector,
// including invalid skills (so they can be inspected).
func findSkill(res discovery.Result, selector string) (discovery.DiscoveredSkill, bool) {
	name, path, hasPath := cutSelector(selector)
	want := discovery.NormalizeID(name)
	for _, s := range res.Skills {
		if hasPath && s.RepoPath != path {
			continue
		}
		if s.ID == want || s.ID == name || discovery.NormalizeID(s.DisplayName) == want {
			return s, true
		}
	}
	return discovery.DiscoveredSkill{}, false
}

// cutSelector splits a "name@path" selector into its parts.
func cutSelector(selector string) (name, path string, hasPath bool) {
	name, path, hasPath = strings.Cut(selector, "@")
	return name, path, hasPath
}
