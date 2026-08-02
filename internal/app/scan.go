package app

import (
	"context"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/store"
)

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
		return installer.New(a.git, cache.New(filepath.Join(stateDirName, "cache")), store.New(filepath.Join(stateDirName, "store"))).WithScanCache(a.scans)
	}
	return installer.New(a.git, cache.New(cacheDir), store.New(filepath.Join(cfgDir, "store"))).WithScanCache(a.scans)
}
