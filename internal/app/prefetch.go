package app

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// prefetchConcurrency bounds parallel source prefetches. Four keeps a
// 100-source run polite to any single forge while collapsing wall time to
// roughly the slowest source.
const prefetchConcurrency = 4

// prefetchJob is one distinct (source, ref, pin) unit worth resolving and
// fetching once, on behalf of every entry that shares it.
type prefetchJob struct {
	name string
	e    skillslock.Entry
}

// maybePrefetch runs the pre-flight source prefetch unless the run cannot
// (offline) or must not (dry-run) touch the network, or there is nothing to
// warm. names is the run's already-sorted entry list (installAllLockEntries
// computed it); reusing it avoids a second clone-and-sort of the lock.
func (a *App) maybePrefetch(ctx context.Context, p *project, lf *skillslock.State, l *skillslock.Lock, req InstallFromLockRequest, names []string) {
	if req.DryRun || req.Offline {
		return
	}
	jobs := a.planPrefetch(p, lf, l, req, names)
	if len(jobs) == 0 {
		// Nothing to warm (fully up to date, or every remaining entry is
		// foreign under --frozen-lockfile): no phase, no phantom UI line.
		return
	}
	emitRunPhase(req.Progress, InstallPhasePrefetching, len(names))
	a.runPrefetch(ctx, p, lf, req.Root, jobs)
}

// planPrefetch selects the distinct units the run will need, deduplicated
// across entries that share a source. Entries a --frozen-lockfile run will
// reject before ever touching the network (a foreign entry has no gskill
// extension; lockEntryTargets fails it outright) are excluded rather than
// prefetched and discarded.
func (a *App) planPrefetch(p *project, lf *skillslock.State, l *skillslock.Lock, req InstallFromLockRequest, names []string) []prefetchJob {
	seen := map[string]bool{}
	var jobs []prefetchJob
	for _, name := range names {
		e, ok := l.Entry(name)
		if !ok || (req.Frozen && e.Ext == nil) || !a.entryNeedsNetwork(p, lf, name, e, req) {
			continue
		}
		key := prefetchKey(lf, name, e)
		if seen[key] {
			continue
		}
		seen[key] = true
		jobs = append(jobs, prefetchJob{name: name, e: e})
	}
	return jobs
}

// runPrefetch warms the resolution memo and commit cache for jobs in
// parallel. It is purely an optimization: every error except cancellation is
// swallowed, because the sequential per-skill loop re-hits the same
// memoized answer and remains the single authority for failure
// classification, phases, and exit codes.
func (a *App) runPrefetch(ctx context.Context, p *project, lf *skillslock.State, root string, jobs []prefetchJob) {
	// A scope's installer is identical for every job that uses it; build
	// each one once instead of re-resolving config/cache/store dirs per
	// goroutine.
	insts := map[string]*installer.Installer{}
	scopeOf := func(e skillslock.Entry) string {
		if e.Ext != nil {
			return e.Ext.Scope
		}
		return ""
	}
	for _, j := range jobs {
		scope := scopeOf(j.e)
		if _, ok := insts[scope]; !ok {
			insts[scope] = a.installerForScope(p, scope)
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(prefetchConcurrency)
	// Concurrent goroutines must not race the renderer: byte-level progress
	// is silenced here; the run-level prefetching phase (emitted by the
	// caller) and the per-skill phases afterwards tell the story.
	gctx = progress.WithSink(gctx, nil)
	for _, j := range jobs {
		if gctx.Err() != nil {
			// Cancelled: don't start jobs that would immediately fail
			// against a dead context — the sequential loop is about to
			// report every entry not-attempted anyway.
			break
		}
		inst := insts[scopeOf(j.e)]
		g.Go(func() error {
			a.prefetchOne(gctx, inst, lf, root, j.name, j.e)
			return gctx.Err()
		})
	}
	_ = g.Wait()
}

// prefetchOne resolves one entry's source (populating the run memo) and
// warms the commit cache. Failures are logged at debug and otherwise
// ignored — see runPrefetch.
func (a *App) prefetchOne(ctx context.Context, inst *installer.Installer, lf *skillslock.State, root, name string, e skillslock.Entry) {
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
	ireq := a.installRequest(root, ref, rev, nil, scope, "")
	if err := inst.EnsureCached(ctx, ireq); err != nil {
		a.log.Debug("prefetch fetch", "skill", name, "error", err)
	}
}

// entryNeedsNetwork reports whether the full pipeline could touch the
// network for this entry. It is a conservative approximation of the
// up-to-date fast path (lockEntryUpToDate): prefer a wasted prefetch (the
// loop then hits a warm cache) over a missed one (the loop resolves/fetches
// sequentially, as before) — so it deliberately checks existence
// (contentHas) rather than paying lockEntryUpToDate's full content-hash
// verification a second time here.
func (a *App) entryNeedsNetwork(p *project, lf *skillslock.State, name string, e skillslock.Entry, req InstallFromLockRequest) bool {
	prior, ok := lf.Skills[name]
	if !ok || e.ComputedHash == "" {
		return true
	}
	if req.Agents != nil {
		ids := normalizeAgentIDs(req.Agents)
		if len(ids) == 0 && len(prior.Installation.Agents) > 0 {
			// Genuine narrow-to-zero (FR-012, lockEntryTargets/
			// narrowEntryToZeroAgents): a pure local unlink with no
			// resolution, fetch, or hash re-verification — network access
			// here would be exactly the cold-cache network access FR-018
			// forbids for this path.
			return false
		}
		if !sameStringSet(ids, prior.Installation.Agents) {
			// A non-empty explicit agent-set change falls through to the
			// full pipeline regardless of content state (lockEntryUpToDate,
			// FR-012): prefetch so that fallthrough isn't also an unwarmed
			// sequential fetch.
			return true
		}
	} else if !sameStringSet(entryAgents(e), prior.Installation.Agents) {
		// No explicit selection, but the lock's declared agents still
		// disagree with what's recorded installed (e.g. gskill.agents was
		// hand-edited) — lockEntryUpToDate falls through to the full
		// pipeline in this case too (lockinstall.go's up-to-date fast path).
		return true
	}
	return !p.contentHas(prior.Resolved.ContentHash)
}

// entrySourceRef returns the source URL and requested ref an entry resolves
// against, applying the same gskill-extension precedence freshResolveLockEntry
// uses: the recorded pin's ext block overrides the plain lock fields when set.
func entrySourceRef(e skillslock.Entry) (src, ref string) {
	src, ref = e.Source, e.Ref
	if e.Ext != nil {
		if e.Ext.SourceURL != "" {
			src = e.Ext.SourceURL
		}
		if ref == "" {
			ref = e.Ext.Ref
		}
	}
	return src, ref
}

// prefetchKey identifies one remote resolution+fetch unit: entries agreeing
// on source, requested ref, and recorded pin share one prefetch.
func prefetchKey(lf *skillslock.State, name string, e skillslock.Entry) string {
	src, ref := entrySourceRef(e)
	pin := ""
	if prior, ok := lf.Skills[name]; ok {
		pin = prior.Resolved.Commit
	}
	return src + "\x00" + ref + "\x00" + pin
}
