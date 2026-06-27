package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLock_RecomputeHonorsPinsWithoutBumping(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Publish a newer in-constraint version.
	gitRun(t, repo, "tag", "v1.1.0")

	// `lock` recomputes without bumping an unchanged declaration.
	if _, stderr, code := runGskill(t, proj, "lock"); code != 0 {
		t.Fatalf("lock: %s", stderr)
	}

	lock := string(readFile(t, filepath.Join(proj, "gskill.lock")))
	if !strings.Contains(lock, `"version": "1.0.0"`) {
		t.Errorf("lock bumped the pinned version; want it to stay 1.0.0:\n%s", lock)
	}
}
