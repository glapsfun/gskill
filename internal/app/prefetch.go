package app

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// prefetchConcurrency bounds parallel source prefetches. Four keeps a
// 100-source run polite to any single forge while collapsing wall time to
// roughly the slowest source.
const prefetchConcurrency = 4

// maybePrefetch runs the pre-flight source prefetch unless the run cannot
// (offline) or must not (dry-run) touch the network.
func (a *App) maybePrefetch(ctx context.Context, p *project, lf *skillslock.State, l *skillslock.Lock, req InstallFromLockRequest, total int) {
	if req.DryRun || req.Offline {
		return
	}
	emitRunPhase(req.Progress, InstallPhasePrefetching, total)
	a.prefetchLockEntries(ctx, p, lf, l, req)
}

// prefetchLockEntries warms the resolution memo and commit cache for every
// distinct source a lock run will need, in parallel. It is purely an
// optimization: every error except cancellation is swallowed, because the
// sequential per-skill loop re-hits the same memoized answer and remains the
// single authority for failure classification, phases, and exit codes.
func (a *App) prefetchLockEntries(ctx context.Context, p *project, lf *skillslock.State, l *skillslock.Lock, req InstallFromLockRequest) {
	type job struct {
		name string
		e    skillslock.Entry
	}
	seen := map[string]bool{}
	var jobs []job
	for _, name := range sortedLockNames(l) {
		e, ok := l.Entry(name)
		if !ok || !a.entryNeedsNetwork(p, lf, name, e) {
			continue
		}
		key := prefetchKey(lf, name, e)
		if seen[key] {
			continue
		}
		seen[key] = true
		jobs = append(jobs, job{name: name, e: e})
	}
	if len(jobs) == 0 {
		return
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(prefetchConcurrency)
	// Concurrent goroutines must not race the renderer: byte-level progress
	// is silenced here; the run-level prefetching phase (emitted by the
	// caller) and the per-skill phases afterwards tell the story.
	gctx = progress.WithSink(gctx, nil)
	for _, j := range jobs {
		g.Go(func() error {
			a.prefetchOne(gctx, p, lf, j.name, j.e, req)
			return gctx.Err()
		})
	}
	_ = g.Wait()
}

// prefetchOne resolves one entry's source (populating the run memo) and
// warms the commit cache. Failures are logged at debug and otherwise
// ignored — see prefetchLockEntries.
func (a *App) prefetchOne(ctx context.Context, p *project, lf *skillslock.State, name string, e skillslock.Entry, req InstallFromLockRequest) {
	ref, rev, _, err := a.resolveLockEntry(ctx, lf, name, e)
	if err != nil {
		a.log.Debug("prefetch resolve", "skill", name, "error", err)
		return
	}
	scope := ""
	if e.Ext != nil {
		scope = e.Ext.Scope
	}
	ref.Path = skillDirOf(e.SkillPath)
	ireq := a.installRequest(req.Root, ref, rev, nil, scope, "")
	ireq.Offline = req.Offline
	if err := a.installerForScope(p, scope).EnsureCached(ctx, ireq); err != nil {
		a.log.Debug("prefetch fetch", "skill", name, "error", err)
	}
}

// entryNeedsNetwork reports whether the full pipeline could touch the
// network for this entry. It is a conservative approximation of the
// up-to-date fast path: prefer a wasted prefetch (the loop then hits a warm
// cache) over a missed one (the loop fetches sequentially, as before).
func (a *App) entryNeedsNetwork(p *project, lf *skillslock.State, name string, e skillslock.Entry) bool {
	prior, ok := lf.Skills[name]
	if !ok || e.ComputedHash == "" {
		return true
	}
	if !p.contentHas(prior.Resolved.ContentHash) {
		return true
	}
	return !a.storedContentUpToDate(p, prior.Resolved.ContentHash, e.ComputedHash)
}

// prefetchKey identifies one remote resolution+fetch unit: entries agreeing
// on source, requested ref, and recorded pin share one prefetch.
func prefetchKey(lf *skillslock.State, name string, e skillslock.Entry) string {
	src, ref := e.Source, e.Ref
	if e.Ext != nil {
		if e.Ext.SourceURL != "" {
			src = e.Ext.SourceURL
		}
		if ref == "" {
			ref = e.Ext.Ref
		}
	}
	pin := ""
	if prior, ok := lf.Skills[name]; ok {
		pin = prior.Resolved.Commit
	}
	return src + "\x00" + ref + "\x00" + pin
}
