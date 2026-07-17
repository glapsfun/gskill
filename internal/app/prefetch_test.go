package app_test

import (
	"context"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
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
