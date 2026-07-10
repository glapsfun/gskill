package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dirMTime returns the modification time snapshot of a path for change detection.
func dirMTime(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime().UnixNano()
}

// TestSyncReconcile_LockDrivenRestore covers US2 scenario 1: a lock declaring
// a skill for two agents is fully re-materialized by sync after the installed
// state is wiped.
func TestSyncReconcile_LockDrivenRestore(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude,codex"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Wipe the materialized state; the committed lock must restore it.
	for _, d := range []string{".claude", ".codex", ".agents"} {
		if err := os.RemoveAll(filepath.Join(proj, d)); err != nil {
			t.Fatal(err)
		}
	}
	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}
	requireCounts(t, proj, 1, 1)
	requireResolvesActive(t, proj, ".claude", "demo")
	requireResolvesActive(t, proj, ".codex", "demo")
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, ".agents/skills/demo") {
		t.Errorf("lockfile missing active path:\n%s", lock)
	}
}

// TestSyncReconcile_Idempotent covers US2 scenario 4 / SC-004: a second sync on
// a matching project makes no changes and reports up to date.
func TestSyncReconcile_Idempotent(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("first sync: %s", stderr)
	}

	lockPath := filepath.Join(proj, "skills-lock.json")
	before := dirMTime(t, lockPath)

	stdout, stderr, code := runGskill(t, proj, "--json", "sync")
	if code != 0 {
		t.Fatalf("second sync: %s", stderr)
	}
	if !strings.Contains(stdout, `"up_to_date": true`) {
		t.Errorf("second sync not reported up to date:\n%s", stdout)
	}
	if after := dirMTime(t, lockPath); after != before {
		t.Errorf("idempotent sync rewrote the lockfile (mtime changed)")
	}
}

// TestSyncReconcile_AddAgentOnlyMissingTarget covers US2 scenario 2 / SC-003:
// adding an agent to an installed skill creates only the new target.
func TestSyncReconcile_AddAgentOnlyMissingTarget(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	claudeTarget := filepath.Join(proj, ".claude", "skills", "demo")
	claudeBefore := dirMTime(t, claudeTarget)

	// Declare codex as well via a pure agent-add (no re-resolve).
	if _, stderr, code := runGskill(t, proj, "add", repo, "--skill", "demo", "--agent", "codex"); code != 0 {
		t.Fatalf("agent add: %s", stderr)
	}
	requireCounts(t, proj, 1, 1) // no duplicate store/active
	requireResolvesActive(t, proj, ".codex", "demo")
	if after := dirMTime(t, claudeTarget); after != claudeBefore {
		t.Errorf("existing claude target was rewritten when only codex was added")
	}
	// The lock records the union.
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"claude"`) || !strings.Contains(lock, `"codex"`) {
		t.Errorf("lock does not record both agents:\n%s", lock)
	}
}

// TestSyncReconcile_UnlinkRemovesAgentTarget covers US2 scenario 3 / SC-008:
// unlinking one agent removes only that agent's target, keeping the active
// entry and other agents.
func TestSyncReconcile_UnlinkRemovesAgentTarget(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude,codex"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	if _, stderr, code := runGskill(t, proj, "unlink", "demo", "--agent", "codex"); code != 0 {
		t.Fatalf("unlink: %s", stderr)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".codex", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("codex target not removed (err=%v)", err)
	}
	requireResolvesActive(t, proj, ".claude", "demo") // kept
	if n := countActiveEntries(t, proj); n != 1 {
		t.Errorf("active entry count = %d, want 1 (retained)", n)
	}
}

// TestSyncReconcile_LegacyMigration covers research R9: an agent target that is a
// symlink directly into the store is re-pointed through a new active entry.
func TestSyncReconcile_LegacyMigration(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Simulate a pre-active-layer install: point the claude target straight at
	// the store and drop the active entry.
	claudeTarget := filepath.Join(proj, ".claude", "skills", "demo")
	storePath, err := filepath.EvalSymlinks(claudeTarget)
	if err != nil {
		t.Fatalf("eval store path: %v", err)
	}
	if err := os.Remove(claudeTarget); err != nil {
		t.Fatalf("rm target: %v", err)
	}
	if err := os.Symlink(storePath, claudeTarget); err != nil {
		t.Fatalf("legacy symlink: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(proj, ".agents")); err != nil {
		t.Fatalf("rm active: %v", err)
	}

	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("reconcile sync: %s", stderr)
	}
	requireResolvesActive(t, proj, ".claude", "demo") // re-pointed through active
	if n := countActiveEntries(t, proj); n != 1 {
		t.Errorf("active entry not recreated (count=%d)", n)
	}
}
