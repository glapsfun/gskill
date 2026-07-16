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

	// Wipe all local state (store + cache + installs) and use a fresh gskill
	// home (empty global store): an offline restore on a truly cold machine
	// fails (exit 5). With a warm global store it would legitimately succeed
	// (spec 015 FR-019), which is why the home must be private here.
	for _, d := range []string{".gskill", ".agents", ".claude"} {
		if err := os.RemoveAll(filepath.Join(proj, d)); err != nil {
			t.Fatal(err)
		}
	}
	_, _, code := runGskillWithApp(t, newAppWithHome(t), proj, "--offline", "install", "--frozen-lockfile")
	if code != 5 {
		t.Errorf("exit code = %d, want 5 (source unavailable offline)", code)
	}
}
