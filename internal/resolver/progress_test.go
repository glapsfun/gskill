package resolver_test

import (
	"context"
	"testing"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
)

func recording(events *[]progress.Event) context.Context {
	return progress.WithSink(context.Background(), func(e progress.Event) {
		*events = append(*events, e)
	})
}

// TestResolve_EmitsProgressForNetworkResolution: a version-constraint resolve
// does an ls-remote round-trip and must report Resolving then Resolved with
// the pinned commit — every caller gets it, no per-site emission needed.
func TestResolve_EmitsProgressForNetworkResolution(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{{Name: "v1.0.0", Commit: "c1"}}}
	var events []progress.Event
	rev, _, err := resolver.Resolve(recording(&events), runner, gitRef(), resolver.Requested{Version: "^1.0.0"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(events) != 2 ||
		events[0].Phase != progress.PhaseResolving ||
		events[1].Phase != progress.PhaseResolved {
		t.Fatalf("events = %+v, want Resolving then Resolved", events)
	}
	if events[1].Commit != rev.Commit {
		t.Errorf("Resolved commit = %q, want %q", events[1].Commit, rev.Commit)
	}
	if events[0].Repo != "acme/widgets" {
		t.Errorf("Resolving repo = %q, want acme/widgets", events[0].Repo)
	}
}

// TestResolve_CommitPinEmitsNothing: an explicit commit pin resolves without
// any network round-trip, so reporting "resolving …" would describe work
// that never happens.
func TestResolve_CommitPinEmitsNothing(t *testing.T) {
	t.Parallel()

	var events []progress.Event
	_, _, err := resolver.Resolve(recording(&events), fakeRunner{}, gitRef(),
		resolver.Requested{Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("commit-pinned resolve emitted %+v, want nothing", events)
	}
}

// TestResolve_LocalEmitsNothing: local sources resolve instantly.
func TestResolve_LocalEmitsNothing(t *testing.T) {
	t.Parallel()

	ref, err := source.Parse(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var events []progress.Event
	if _, _, err := resolver.Resolve(recording(&events), fakeRunner{}, ref, resolver.Requested{}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("local resolve emitted %+v, want nothing", events)
	}
}
