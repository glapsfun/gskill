package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineRestore_WarmCacheSucceedsColdCacheFails(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	// add warms the cache with the resolved commit.
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Offline frozen restore succeeds on a warm cache (SC-003).
	if _, stderr, code := runGskill(t, proj, "--offline", "install", "--frozen-lockfile"); code != 0 {
		t.Fatalf("offline restore with warm cache failed: %s", stderr)
	}

	// Clear the cache; an offline restore of an uncached commit fails (exit 5).
	if err := os.RemoveAll(filepath.Join(proj, ".gskill", "cache")); err != nil {
		t.Fatal(err)
	}
	_, _, code := runGskill(t, proj, "--offline", "install", "--frozen-lockfile")
	if code != 5 {
		t.Errorf("exit code = %d, want 5 (source unavailable offline)", code)
	}
}
