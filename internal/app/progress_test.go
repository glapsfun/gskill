package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/testutil"
)

// gitSkillRepo creates a local git repository holding one committed skill and
// returns its path. Local git repos are promoted to git-type sources, so
// discovery and install exercise the real fetch/cache pipeline.
func gitSkillRepo(t *testing.T, name string) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// The repo directory carries the skill's identity (root-level skills are
	// keyed by the repo name), so it needs a stable basename.
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "-b", "main")
	body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "initial")
	return repo
}

func sinkCtx(events *[]progress.Event) context.Context {
	return progress.WithSink(context.Background(), func(e progress.Event) {
		*events = append(*events, e)
	})
}

func phases(events []progress.Event) []progress.Phase {
	out := make([]progress.Phase, 0, len(events))
	for _, e := range events {
		out = append(out, e.Phase)
	}
	return out
}

// TestDiscoverSource_EmitsResolveAndFetchProgress: the add path's download
// site reports resolving, the resolved commit, the fetch, and completion —
// and a warm cache reports Cached instead of a fetch.
func TestDiscoverSource_EmitsResolveAndFetchProgress(t *testing.T) {
	t.Parallel()

	src := gitSkillRepo(t, "alpha")
	root := projectWithAgent(t)
	a := onboardApp()

	var events []progress.Event
	ctx := sinkCtx(&events)
	if _, err := a.DiscoverSource(ctx, app.DiscoverRequest{Root: root, Source: src}); err != nil {
		t.Fatalf("DiscoverSource: %v", err)
	}

	// git may interleave parser-level phases (Counting/Receiving/Deltas) even
	// on a local fetch; assert the structural milestones around them.
	got := milestones(events)
	want := []progress.Phase{progress.PhaseResolving, progress.PhaseResolved, progress.PhaseFetching, progress.PhaseDone}
	if !slices.Equal(got, want) {
		t.Fatalf("milestones = %v, want %v (all events: %+v)", got, want, events)
	}
	if events[1].Commit == "" {
		t.Error("Resolved event missing the commit")
	}
	if events[2].Repo == "" || events[2].Commit == "" {
		t.Errorf("Fetching event not stamped: %+v", events[2])
	}

	// Second discovery: warm cache, no fetch.
	events = nil
	ctx = sinkCtx(&events)
	if _, err := a.DiscoverSource(ctx, app.DiscoverRequest{Root: root, Source: src}); err != nil {
		t.Fatalf("cached DiscoverSource: %v", err)
	}
	got = milestones(events)
	want = []progress.Phase{progress.PhaseResolving, progress.PhaseResolved, progress.PhaseCached}
	if !slices.Equal(got, want) {
		t.Fatalf("cached milestones = %v, want %v", got, want)
	}
}

// milestones filters out the git-parser phases, keeping the structural ones.
func milestones(events []progress.Event) []progress.Phase {
	var out []progress.Phase
	for _, p := range phases(events) {
		switch p {
		case progress.PhaseCounting, progress.PhaseReceiving, progress.PhaseDeltas:
			continue
		case progress.PhaseResolving, progress.PhaseResolved, progress.PhaseCached,
			progress.PhaseFetching, progress.PhaseDone:
			out = append(out, p)
		}
	}
	return out
}

// twoSkillProject installs alpha and beta from two local git repos and
// returns the project root and app, for exercising the reconcile commands.
func twoSkillProject(t *testing.T) (string, *app.App) {
	t.Helper()

	root := projectWithAgent(t)
	a := onboardApp()
	for _, name := range []string{"alpha", "beta"} {
		src := gitSkillRepo(t, name)
		if _, err := a.Add(context.Background(), app.AddRequest{Root: root, Source: src, All: true}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	return root, a
}

// assertStamped checks that every event carries a skill name and the [k/N]
// counter, and that both skills appear at their sorted positions.
func assertStamped(t *testing.T, events []progress.Event) {
	t.Helper()

	if len(events) == 0 {
		t.Fatal("no progress events emitted")
	}
	seen := map[string][2]int{}
	for _, e := range events {
		if e.Skill == "" {
			t.Errorf("event missing skill stamp: %+v", e)
			continue
		}
		if e.Count != 2 {
			t.Errorf("event Count = %d, want 2: %+v", e.Count, e)
		}
		seen[e.Skill] = [2]int{e.Index, e.Count}
	}
	if got := seen["alpha"]; got != [2]int{1, 2} {
		t.Errorf("alpha stamped %v, want [1 2]", got)
	}
	if got := seen["beta"]; got != [2]int{2, 2} {
		t.Errorf("beta stamped %v, want [2 2]", got)
	}
}

// TestInstall_StampsSkillAndCounter: every command that walks the per-skill
// install pipeline stamps its events with the skill name and [k/N] position,
// so the renderer can draw a multi-repo counter and per-skill ✓ lines.
func TestInstall_StampsSkillAndCounter(t *testing.T) {
	t.Parallel()

	root, a := twoSkillProject(t)
	// Break the targets so install re-materializes (a healthy chain
	// short-circuits with no per-skill events).
	for _, name := range []string{"alpha", "beta"} {
		if err := os.RemoveAll(filepath.Join(root, ".gskill")); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(root, ".claude", "skills", name)); err != nil {
			t.Fatal(err)
		}
	}
	var events []progress.Event
	if _, err := a.InstallFromLock(sinkCtx(&events), app.InstallFromLockRequest{Root: root}); err != nil {
		t.Fatalf("InstallFromLock: %v", err)
	}
	assertStamped(t, events)
}

func TestUpdate_StampsSkillAndCounter(t *testing.T) {
	t.Parallel()

	root, a := twoSkillProject(t)
	var events []progress.Event
	if _, err := a.Update(sinkCtx(&events), root, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	assertStamped(t, events)
}

func TestSync_StampsSkillAndCounter(t *testing.T) {
	t.Parallel()

	root, a := twoSkillProject(t)
	// A healthy chain short-circuits with no installs (and no events); break
	// the agent targets so sync actually re-materializes both skills.
	for _, name := range []string{"alpha", "beta"} {
		if err := os.RemoveAll(filepath.Join(root, ".claude", "skills", name)); err != nil {
			t.Fatal(err)
		}
	}
	var events []progress.Event
	if _, err := a.Sync(sinkCtx(&events), app.SyncRequest{Root: root}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	assertStamped(t, events)
}
