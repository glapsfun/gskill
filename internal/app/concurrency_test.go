package app_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestConcurrency_SameObjectTwoProjects is spec 015 US8 scenario 1 and
// quickstart S8: two simultaneous installs of the same missing content into
// two projects both succeed, and exactly one physical object per skill
// exists afterwards.
func TestConcurrency_SameObjectTwoProjects(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		writeLockOnly(t, root, repo, ha, hb)
	}

	var wg sync.WaitGroup
	errs := make([]error, len(roots))
	for i, root := range roots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = installLock(t, a, root, false)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent install %d: %v", i, err)
		}
	}
	objects := listStoreObjects(t, h)
	if len(objects) != 2 {
		t.Errorf("store objects = %v, want exactly 2 (one per skill, no duplicates)", objects)
	}
	for _, root := range roots {
		assertAgentTargets(t, root, "alpha", "beta")
	}
}

// TestConcurrency_SameProjectSerializes is US8 scenario 3: two runs against
// one project never interleave — both succeed (the second waits on the
// project lock) and the result is consistent.
func TestConcurrency_SameProjectSerializes(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = installLock(t, a, root, false)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("run %d: %v", i, err)
		}
	}
	assertAgentTargets(t, root, "alpha", "beta")
	assertLockEnriched(t, root, map[string]string{"alpha": ha, "beta": hb})
}

// TestConcurrency_DifferentProjectsDistinctLocks (spec 015 FR-030): two
// global-scope projects must not share one mutate-lock file — a fixed name
// in the shared locks dir would serialize the whole machine.
func TestConcurrency_DifferentProjectsDistinctLocks(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		writeLockOnly(t, root, repo, ha, hb)
		if _, err := installLock(t, a, root, false); err != nil {
			t.Fatal(err)
		}
	}

	locks, err := os.ReadDir(filepath.Join(h, "locks"))
	if err != nil {
		t.Fatal(err)
	}
	project := 0
	for _, l := range locks {
		if len(l.Name()) > 8 && l.Name()[:8] == "project-" {
			project++
		}
	}
	if project < 2 {
		t.Errorf("project lock files = %d, want one per project (locks: %v)", project, locks)
	}
}
