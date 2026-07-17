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
	sf    singleflight.Group
}

// result pairs a memoized answer with the error it arrived with.
type result[T any] struct {
	val T
	err error
}

// memoized returns the cached answer for key in table, or fetches, records,
// and returns it. Concurrent callers of the same key share one in-flight
// fetch via the store's singleflight group; kind namespaces the flight keys
// across tables.
func memoized[T any](s *memoStore, table map[string]result[T], kind, key string, fetch func() (T, error)) (T, error) {
	v, err, _ := s.sf.Do(kind+"\x00"+key, func() (any, error) {
		s.mu.Lock()
		if r, ok := table[key]; ok {
			s.mu.Unlock()
			return r.val, r.err
		}
		s.mu.Unlock()
		val, err := fetch()
		s.mu.Lock()
		table[key] = result[T]{val: val, err: err}
		s.mu.Unlock()
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
		tags:  map[string]result[[]TagRef]{},
		heads: map[string]result[[]BranchRef]{},
		refs:  map[string]result[string]{},
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

// Fresh returns the runner beneath any memoization, for the one caller that
// must re-ask the remote: the computedHash-mismatch retry, whose whole point
// is seeing upstream state newer than this run's memoized view.
func Fresh(r Runner) Runner {
	if m, ok := r.(Memoized); ok {
		return m.inner
	}
	return r
}

// LsRemoteTags returns the run's memoized tag listing for url, fetching it
// once on first use.
func (m Memoized) LsRemoteTags(ctx context.Context, url string) ([]TagRef, error) {
	s := memoFrom(ctx)
	if s == nil {
		return m.inner.LsRemoteTags(ctx, url)
	}
	return memoized(s, s.tags, "tags", url, func() ([]TagRef, error) {
		return m.inner.LsRemoteTags(ctx, url)
	})
}

// LsRemoteHeads returns the run's memoized branch listing for url.
func (m Memoized) LsRemoteHeads(ctx context.Context, url string) ([]BranchRef, error) {
	s := memoFrom(ctx)
	if s == nil {
		return m.inner.LsRemoteHeads(ctx, url)
	}
	return memoized(s, s.heads, "heads", url, func() ([]BranchRef, error) {
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
	return memoized(s, s.refs, "ref", url+"\x00"+ref, func() (string, error) {
		return m.inner.ResolveRef(ctx, url, ref)
	})
}

// FetchCommit always delegates: it materializes into a caller-owned dest,
// so sharing one result across callers would be wrong.
func (m Memoized) FetchCommit(ctx context.Context, url, commit, dest string) error {
	return m.inner.FetchCommit(ctx, url, commit, dest)
}
