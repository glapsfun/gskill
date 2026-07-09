// Package progress carries a download-progress sink through context.Context
// so the git/installer/app layers can report fetch progress to the CLI
// without any signature changes. Every emitter no-ops when no
// sink is installed, so non-interactive runs are structurally unchanged.
package progress

import "context"

// Phase identifies where in a source download an Event was emitted.
type Phase int

const (
	// PhaseResolving marks a ref-resolution round-trip (ls-remote) starting.
	PhaseResolving Phase = iota
	// PhaseResolved marks the revision as known; Commit is set.
	PhaseResolved
	// PhaseCached marks a content-cache hit: no network fetch will happen.
	PhaseCached
	// PhaseFetching marks a git fetch starting for Repo at Commit.
	PhaseFetching
	// PhaseCounting mirrors git's "Counting/Compressing objects" progress.
	PhaseCounting
	// PhaseReceiving mirrors git's "Receiving objects" progress.
	PhaseReceiving
	// PhaseDeltas mirrors git's "Resolving deltas" progress.
	PhaseDeltas
	// PhaseDone marks the fetched tree as materialized into the cache.
	PhaseDone
)

// Event is one progress observation. Layers closer to the CLI stamp the
// identifying fields (Repo, Skill, Index, Count) via Stamp.
type Event struct {
	Repo   string // display name, e.g. "acme/skills"
	Skill  string // manifest skill name; empty outside `install`
	Index  int    // 1-based repo counter for [k/N]; 0 outside `install`
	Count  int    // total repos for [k/N]; 0 outside `install`
	Commit string // resolved commit when known

	Phase   Phase
	Percent int    // 0..100; -1 when git reported no totals
	Objects int64  // "a" of git's "(a/b)" counter
	Total   int64  // "b" of git's "(a/b)" counter; 0 = unknown
	Detail  string // verbatim human tail from git, e.g. "4.1 MiB | 2.3 MiB/s"
}

// Sink receives progress events. Implementations must be safe to call from
// multiple goroutines (git's stderr is streamed off the exec goroutine).
type Sink func(Event)

type sinkKey struct{}

// WithSink returns a context carrying s as the progress sink.
func WithSink(ctx context.Context, s Sink) context.Context {
	return context.WithValue(ctx, sinkKey{}, s)
}

// FromContext returns the sink installed on ctx, or nil.
func FromContext(ctx context.Context) Sink {
	s, _ := ctx.Value(sinkKey{}).(Sink)
	return s
}

// Emit delivers e to the sink on ctx, if any.
func Emit(ctx context.Context, e Event) {
	if s := FromContext(ctx); s != nil {
		s(e)
	}
}

// Stamp wraps the sink on ctx so decorate runs on every event before it is
// forwarded — a layer that knows the repo or the [k/N] position fills those
// fields in for everything emitted below it. A sink-less ctx is returned
// unchanged.
func Stamp(ctx context.Context, decorate func(*Event)) context.Context {
	s := FromContext(ctx)
	if s == nil {
		return ctx
	}
	return WithSink(ctx, func(e Event) {
		decorate(&e)
		s(e)
	})
}
