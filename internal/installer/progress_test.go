package installer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/store"
)

// skillWriterRunner fakes git by writing a SKILL.md into the fetch dest.
type skillWriterRunner struct{}

func (skillWriterRunner) LsRemoteTags(context.Context, string) ([]git.TagRef, error) {
	return nil, nil
}

func (skillWriterRunner) LsRemoteHeads(context.Context, string) ([]git.BranchRef, error) {
	return nil, nil
}

func (skillWriterRunner) ResolveRef(_ context.Context, _, ref string) (string, error) {
	return ref, nil
}

func (skillWriterRunner) FetchCommit(_ context.Context, _, _, dest string) error {
	body := "---\nname: demo\ndescription: a skill\n---\n# demo\n"
	return os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(body), 0o600)
}

const testCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func gitRequest(t *testing.T, offline bool) installer.Request {
	t.Helper()
	ref, err := source.Parse("github.com/acme/skills")
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	return installer.Request{
		Ref:      ref,
		Revision: resolver.Revision{Commit: testCommit},
		Offline:  offline,
	}
}

func recordingSink(events *[]progress.Event) context.Context {
	return progress.WithSink(context.Background(), func(e progress.Event) {
		*events = append(*events, e)
	})
}

// TestMaterialize_EmitsFetchingThenDone: a cold-cache materialize reports the
// fetch starting and finishing, stamped with the repo display name and commit.
func TestMaterialize_EmitsFetchingThenDone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inst := installer.New(skillWriterRunner{}, cache.New(filepath.Join(root, "cache")), store.New(filepath.Join(root, "store")))

	var events []progress.Event
	ctx := recordingSink(&events)
	if _, err := inst.Discover(ctx, gitRequest(t, false)); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("got %d events, want Fetching then Done: %+v", len(events), events)
	}
	if events[0].Phase != progress.PhaseFetching || events[1].Phase != progress.PhaseDone {
		t.Errorf("phases = %v,%v, want Fetching,Done", events[0].Phase, events[1].Phase)
	}
	for i, e := range events {
		if e.Repo != "acme/skills" {
			t.Errorf("event %d Repo = %q, want acme/skills", i, e.Repo)
		}
		if e.Commit != testCommit {
			t.Errorf("event %d Commit = %q, want the resolved commit", i, e.Commit)
		}
	}
}

// TestMaterialize_CacheHitEmitsCached: a warm cache reports exactly one
// Cached event and never a fetch.
func TestMaterialize_CacheHitEmitsCached(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inst := installer.New(skillWriterRunner{}, cache.New(filepath.Join(root, "cache")), store.New(filepath.Join(root, "store")))

	if _, err := inst.Discover(context.Background(), gitRequest(t, false)); err != nil {
		t.Fatalf("warm-up Discover: %v", err)
	}

	var events []progress.Event
	ctx := recordingSink(&events)
	if _, err := inst.Discover(ctx, gitRequest(t, false)); err != nil {
		t.Fatalf("cached Discover: %v", err)
	}
	if len(events) != 1 || events[0].Phase != progress.PhaseCached {
		t.Fatalf("got %+v, want exactly one Cached event", events)
	}
	if events[0].Repo != "acme/skills" || events[0].Commit != testCommit {
		t.Errorf("cached event not stamped: %+v", events[0])
	}
}

// TestMaterialize_OfflineUncachedEmitsNothing: the offline error path stays
// silent — there is no fetch to report.
func TestMaterialize_OfflineUncachedEmitsNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inst := installer.New(skillWriterRunner{}, cache.New(filepath.Join(root, "cache")), store.New(filepath.Join(root, "store")))

	var events []progress.Event
	ctx := recordingSink(&events)
	_, err := inst.Discover(ctx, gitRequest(t, true))
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("err = %v, want the offline-uncached failure", err)
	}
	if len(events) != 0 {
		t.Errorf("offline error path emitted %+v, want nothing", events)
	}
}
