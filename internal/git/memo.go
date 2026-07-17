package git

import (
	"context"
	"sync"

	"golang.org/x/sync/singleflight"
)

// memoStore holds one run's remote-resolution answers, errors included: a
// run sees one consistent view of every remote, and an unreachable host
// costs one timeout instead of one per dependent skill. FetchCommit is never
// stored — it writes into a caller-owned directory, so commit-level dedup is
// the commit cache's job.
type memoStore struct {
	mu    sync.Mutex
	tags  map[string]result[[]TagRef]
	heads map[string]result[[]BranchRef]
	refs  map[string]result[string]
	// freshKeys marks answers a Fresh runner already re-asked this run: one
	// upstream move costs one refresh, not one per dependent skill.
	freshKeys map[string]bool
	sf        singleflight.Group
}

// cacheable reports whether the call's own context was still live when the
// answer arrived. A call whose context died (parent cancel, prefetch
// cancellation) speaks for the context, not the remote: caching that answer
// would poison every later resolution of the source for the rest of the
// run. A genuine remote error under a live context is real information and
// is cached like any other answer.
func cacheable(ctx context.Context) bool {
	return ctx.Err() == nil
}

// result pairs a memoized answer with the error it arrived with.
type result[T any] struct {
	val T
	err error
}

// memoized returns the cached answer for key in table, or fetches, records,
// and returns it. Concurrent callers of the same key share one in-flight
// fetch via the store's singleflight group; kind namespaces the flight keys
// across tables. ctx is the fetch's own context: its aliveness after fetch
// returns decides whether the answer is fit to record (see cacheable).
func memoized[T any](ctx context.Context, s *memoStore, table map[string]result[T], kind, key string, fetch func() (T, error)) (T, error) {
	v, err, _ := s.sf.Do(kind+"\x00"+key, func() (any, error) {
		s.mu.Lock()
		if r, ok := table[key]; ok {
			s.mu.Unlock()
			return r.val, r.err
		}
		s.mu.Unlock()
		val, err := fetch()
		if cacheable(ctx) {
			s.mu.Lock()
			table[key] = result[T]{val: val, err: err}
			s.mu.Unlock()
		}
		return val, err
	})
	val, _ := v.(T)
	return val, err
}

// refreshed serves a Fresh runner's call: the first ask for a key this run
// bypasses the memoized view, re-asks the remote, and the answer becomes the
// run's view (memoized reads included); later Fresh asks for the same key
// reuse it. Without an armed context it is a plain passthrough. Routed
// through the store's singleflight group (a distinct "fresh" key namespace,
// so it never collides with a concurrent Memoized call on the same
// underlying key) so concurrent Fresh callers for the same key — e.g. N
// sibling skills independently hitting a computedHash mismatch against one
// externally-moved source — collapse to one round trip, not N.
func refreshed[T any](ctx context.Context, table func(*memoStore) map[string]result[T], kind, key string, fetch func() (T, error)) (T, error) {
	s := memoFrom(ctx)
	if s == nil {
		return fetch()
	}
	fkey := "fresh\x00" + kind + "\x00" + key
	v, err, _ := s.sf.Do(fkey, func() (any, error) {
		s.mu.Lock()
		if s.freshKeys[fkey] {
			r := table(s)[key]
			s.mu.Unlock()
			return r.val, r.err
		}
		s.mu.Unlock()
		val, err := fetch()
		if cacheable(ctx) {
			s.mu.Lock()
			table(s)[key] = result[T]{val: val, err: err}
			s.freshKeys[fkey] = true
			s.mu.Unlock()
		}
		return val, err
	})
	val, _ := v.(T)
	return val, err
}

type memoKey struct{}

// WithMemo arms run-scoped memoization for every Memoized runner call made
// under the returned context. One armed context = one command's consistent
// view of each remote; repeated resolutions return the first answer.
// Idempotent: a context already carrying a memo is returned unchanged.
func WithMemo(ctx context.Context) context.Context {
	if ctx.Value(memoKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, memoKey{}, &memoStore{
		tags:      map[string]result[[]TagRef]{},
		heads:     map[string]result[[]BranchRef]{},
		refs:      map[string]result[string]{},
		freshKeys: map[string]bool{},
	})
}

func memoFrom(ctx context.Context) *memoStore {
	s, _ := ctx.Value(memoKey{}).(*memoStore)
	return s
}

// Memoized decorates a Runner with context-scoped memoization (see
// WithMemo). Without an armed context every call passes straight through.
type Memoized struct {
	inner Runner
}

// Memoize wraps r in a Memoized runner. Idempotent.
func Memoize(r Runner) Runner {
	if _, ok := r.(Memoized); ok {
		return r
	}
	return Memoized{inner: r}
}

// Fresh returns a runner for callers that must see upstream state newer than
// the run's memoized view (the computedHash-mismatch retry). Its first ask
// for a key re-asks the remote and refreshes the run memo; later asks — from
// this runner or the memoized one — reuse the refreshed answer, so N sibling
// skills retrying one moved source cost one extra round trip, not N.
func Fresh(r Runner) Runner {
	if m, ok := r.(Memoized); ok {
		return freshRunner(m)
	}
	return r
}

// freshRunner implements Fresh's refresh-once semantics over the context
// memo. Without an armed context every call passes straight through.
type freshRunner struct {
	inner Runner
}

// LsRemoteTags re-asks the remote once per run and refreshes the memo.
func (f freshRunner) LsRemoteTags(ctx context.Context, url string) ([]TagRef, error) {
	return refreshed(ctx, func(s *memoStore) map[string]result[[]TagRef] { return s.tags }, "tags", url,
		func() ([]TagRef, error) { return f.inner.LsRemoteTags(ctx, url) })
}

// LsRemoteHeads re-asks the remote once per run and refreshes the memo.
func (f freshRunner) LsRemoteHeads(ctx context.Context, url string) ([]BranchRef, error) {
	return refreshed(ctx, func(s *memoStore) map[string]result[[]BranchRef] { return s.heads }, "heads", url,
		func() ([]BranchRef, error) { return f.inner.LsRemoteHeads(ctx, url) })
}

// ResolveRef re-asks the remote once per run and refreshes the memo; a
// full-SHA ref stays a local passthrough.
func (f freshRunner) ResolveRef(ctx context.Context, url, ref string) (string, error) {
	if IsFullSHA(ref) {
		return f.inner.ResolveRef(ctx, url, ref)
	}
	return refreshed(ctx, func(s *memoStore) map[string]result[string] { return s.refs }, "ref", url+"\x00"+ref,
		func() (string, error) { return f.inner.ResolveRef(ctx, url, ref) })
}

// FetchCommit always delegates (see Memoized.FetchCommit).
func (f freshRunner) FetchCommit(ctx context.Context, url, commit, dest string) error {
	return f.inner.FetchCommit(ctx, url, commit, dest)
}

// LsRemoteTags returns the run's memoized tag listing for url, fetching it
// once on first use.
func (m Memoized) LsRemoteTags(ctx context.Context, url string) ([]TagRef, error) {
	s := memoFrom(ctx)
	if s == nil {
		return m.inner.LsRemoteTags(ctx, url)
	}
	return memoized(ctx, s, s.tags, "tags", url, func() ([]TagRef, error) {
		return m.inner.LsRemoteTags(ctx, url)
	})
}

// LsRemoteHeads returns the run's memoized branch listing for url.
func (m Memoized) LsRemoteHeads(ctx context.Context, url string) ([]BranchRef, error) {
	s := memoFrom(ctx)
	if s == nil {
		return m.inner.LsRemoteHeads(ctx, url)
	}
	return memoized(ctx, s, s.heads, "heads", url, func() ([]BranchRef, error) {
		return m.inner.LsRemoteHeads(ctx, url)
	})
}

// ResolveRef returns the run's memoized commit for (url, ref). A full-SHA
// ref stays uncached: the runner answers it locally.
func (m Memoized) ResolveRef(ctx context.Context, url, ref string) (string, error) {
	s := memoFrom(ctx)
	if s == nil || IsFullSHA(ref) {
		return m.inner.ResolveRef(ctx, url, ref)
	}
	return memoized(ctx, s, s.refs, "ref", url+"\x00"+ref, func() (string, error) {
		return m.inner.ResolveRef(ctx, url, ref)
	})
}

// FetchCommit always delegates: it materializes into a caller-owned dest,
// so sharing one result across callers would be wrong.
func (m Memoized) FetchCommit(ctx context.Context, url, commit, dest string) error {
	return m.inner.FetchCommit(ctx, url, commit, dest)
}
