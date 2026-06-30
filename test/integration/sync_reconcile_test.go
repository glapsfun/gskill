package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest writes a gskill.toml declaring one skill from source for the
// given agents.
func writeManifest(t *testing.T, proj, source string, agents ...string) {
	t.Helper()
	quoted := make([]string, len(agents))
	for i, a := range agents {
		quoted[i] = `"` + a + `"`
	}
	body := "schema_version = 1\n\n[skills.demo]\nsource = \"" + source + "\"\nversion = \"^1.0.0\"\nagents = [" + strings.Join(quoted, ", ") + "]\n"
	if err := os.WriteFile(filepath.Join(proj, "gskill.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// dirMTime returns the modification time snapshot of a path for change detection.
func dirMTime(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime().UnixNano()
}

// TestSyncReconcile_ManifestDrivenInstall covers US2 scenario 1: a manifest
// declaring a skill for two agents, reconciled from an empty project.
func TestSyncReconcile_ManifestDrivenInstall(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	writeManifest(t, proj, repo, "claude", "codex")

	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}
	requireCounts(t, proj, 1, 1)
	requireResolvesActive(t, proj, ".claude", "demo")
	requireResolvesActive(t, proj, ".codex", "demo")
	lock := string(readFile(t, filepath.Join(proj, "gskill.lock")))
	if !strings.Contains(lock, ".agents/skills/demo") {
		t.Errorf("lockfile missing active_path:\n%s", lock)
	}
}

// TestSyncReconcile_Idempotent covers US2 scenario 4 / SC-004: a second sync on
// a matching project makes no changes and reports up to date.
func TestSyncReconcile_Idempotent(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	writeManifest(t, proj, repo, "claude")
	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("first sync: %s", stderr)
	}

	lockPath := filepath.Join(proj, "gskill.lock")
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
// adding an agent to the manifest and re-syncing creates only the new target.
func TestSyncReconcile_AddAgentOnlyMissingTarget(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	writeManifest(t, proj, repo, "claude")
	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("first sync: %s", stderr)
	}
	claudeTarget := filepath.Join(proj, ".claude", "skills", "demo")
	claudeBefore := dirMTime(t, claudeTarget)

	// Declare codex as well and re-sync.
	writeManifest(t, proj, repo, "claude", "codex")
	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("second sync: %s", stderr)
	}
	requireCounts(t, proj, 1, 1) // no duplicate store/active
	requireResolvesActive(t, proj, ".codex", "demo")
	if after := dirMTime(t, claudeTarget); after != claudeBefore {
		t.Errorf("existing claude target was rewritten when only codex was added")
	}
}

// TestSyncReconcile_PruneRemovesUndesiredAgent covers US2 scenario 3 / SC-008:
// removing an agent from the manifest and syncing with --prune removes only that
// agent's target, keeping the active entry and other agents.
func TestSyncReconcile_PruneRemovesUndesiredAgent(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	writeManifest(t, proj, repo, "claude", "codex")
	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}

	// Drop codex and prune.
	writeManifest(t, proj, repo, "claude")
	if _, stderr, code := runGskill(t, proj, "sync", "--prune"); code != 0 {
		t.Fatalf("sync --prune: %s", stderr)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".codex", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("codex target not pruned (err=%v)", err)
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
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	writeManifest(t, proj, repo, "claude")
	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
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
