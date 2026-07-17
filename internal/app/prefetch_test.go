package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/testutil"
)

// TestInstallFromLock_ResolvesEachSourceOnce: N skills from one source cost
// one source resolution and one fetch — not one per skill (spec: O(skills) →
// O(sources) round trips).
func TestInstallFromLock_ResolvesEachSourceOnce(t *testing.T) {
	t.Parallel()

	repo := gitMultiSkillRepo(t, "widgets", "gcs", "gke", "iam")
	root := projectWithAgentTB(t)
	ctx := context.Background()
	if _, err := onboardApp().Add(ctx, app.AddRequest{Root: root, Source: repo, All: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	stripGskillExt(t, root)

	counting := &testutil.CountingGit{Inner: git.NewSystemRunner()}
	a := countingGitApp(t, counting)
	res, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root, Agents: []string{agent.DefaultID}})
	if err != nil {
		t.Fatalf("InstallFromLock: %v", err)
	}
	if got := len(res.Skills); got != 3 {
		t.Fatalf("skills = %d, want 3", got)
	}
	// Unpinned resolution of one source: ls-remote --tags (finds none) then
	// HEAD — at most 2 round trips for the whole run, not per skill.
	if got := counting.ResolutionCalls(); got > 2 {
		t.Errorf("resolution round trips = %d, want <= 2 for one source", got)
	}
	if got := counting.Fetches.Load(); got != 1 {
		t.Errorf("FetchCommit calls = %d, want 1", got)
	}
}

// TestInstallFromLock_DeadSourceFailsOnceNotPerSkill: an unreachable source
// costs one network attempt for the run; every dependent skill reports the
// same failure through the existing per-skill error machinery.
func TestInstallFromLock_DeadSourceFailsOnceNotPerSkill(t *testing.T) {
	t.Parallel()

	repo := gitMultiSkillRepo(t, "widgets", "gcs", "gke", "iam")
	root := projectWithAgentTB(t)
	ctx := context.Background()
	if _, err := onboardApp().Add(ctx, app.AddRequest{Root: root, Source: repo, All: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	stripGskillExt(t, root)

	counting := &testutil.CountingGit{
		Inner: git.NewSystemRunner(),
		Fail:  context.DeadlineExceeded, // stand-in for a network timeout
	}
	a := countingGitApp(t, counting)
	res, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root, Agents: []string{agent.DefaultID}})
	if err == nil {
		t.Fatal("InstallFromLock succeeded against a dead source")
	}
	failed := 0
	for _, s := range res.Skills {
		if s.Err != nil {
			failed++
		}
	}
	if failed != 3 {
		t.Errorf("failed skills = %d, want 3", failed)
	}
	if got := counting.ResolutionCalls(); got != 1 {
		t.Errorf("network attempts = %d, want 1 (error memoized)", got)
	}
}

// TestInstallFromLock_MultiSourceFetchesEachSourceOnce: S sources × K skills
// cost at most 2 resolution round trips and exactly 1 fetch per source, and
// every skill still installs with deterministic per-skill results.
func TestInstallFromLock_MultiSourceFetchesEachSourceOnce(t *testing.T) {
	t.Parallel()

	root := projectWithAgentTB(t)
	ctx := context.Background()
	seed := onboardApp()
	for _, name := range []string{"alpha", "beta"} {
		// Lock entries are keyed by skill name: unique names per source.
		repo := gitMultiSkillRepo(t, name, "gcs-"+name, "gke-"+name, "iam-"+name)
		if _, err := seed.Add(ctx, app.AddRequest{Root: root, Source: repo, All: true}); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	stripGskillExt(t, root)

	counting := &testutil.CountingGit{Inner: git.NewSystemRunner()}
	a := countingGitApp(t, counting)
	res, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root, Agents: []string{agent.DefaultID}})
	if err != nil {
		t.Fatalf("InstallFromLock: %v", err)
	}
	if got := len(res.Skills); got != 6 {
		t.Fatalf("skills = %d, want 6", got)
	}
	if got := counting.ResolutionCalls(); got > 4 {
		t.Errorf("resolution round trips = %d, want <= 4 for two sources", got)
	}
	if got := counting.Fetches.Load(); got != 2 {
		t.Errorf("FetchCommit calls = %d, want 2 (one per source)", got)
	}
}

// TestInstallFromLock_PreCancelledContextStaysInterrupted: prefetch must not
// change the cancellation contract — a cancelled run reports ErrCancelled
// with every skill not attempted.
func TestInstallFromLock_PreCancelledContextStaysInterrupted(t *testing.T) {
	t.Parallel()

	repo := gitMultiSkillRepo(t, "widgets", "gcs", "gke")
	root := projectWithAgentTB(t)
	ctx := context.Background()
	if _, err := onboardApp().Add(ctx, app.AddRequest{Root: root, Source: repo, All: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	stripGskillExt(t, root)

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	a := countingGitApp(t, &testutil.CountingGit{Inner: git.NewSystemRunner()})
	_, err := a.InstallFromLock(cctx, app.InstallFromLockRequest{Root: root, Agents: []string{agent.DefaultID}})
	if !errors.Is(err, errs.ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

// TestInstallFromLock_OfflineNeverFetches: --offline skips prefetch and
// forbids fetching. Note: unpinned entries still resolve refs before the
// offline guard — a pre-existing leak in resolveLockEntry (it predates the
// memo; the memo now at least collapses it to one round trip per source) —
// so the invariant asserted here is fetches, not all runner calls.
func TestInstallFromLock_OfflineNeverFetches(t *testing.T) {
	t.Parallel()

	repo := gitMultiSkillRepo(t, "widgets", "gcs", "gke")
	root := projectWithAgentTB(t)
	ctx := context.Background()
	if _, err := onboardApp().Add(ctx, app.AddRequest{Root: root, Source: repo, All: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	stripGskillExt(t, root)

	counting := &testutil.CountingGit{Inner: git.NewSystemRunner()}
	a := countingGitApp(t, counting)
	// Cold home + no pins + offline: the run may fail (nothing cached) —
	// that is fine; the invariant under test is the call count, not success.
	_, _ = a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root, Offline: true, Agents: []string{agent.DefaultID}})
	if got := counting.Fetches.Load(); got != 0 {
		t.Errorf("fetches under --offline = %d, want 0", got)
	}
}
