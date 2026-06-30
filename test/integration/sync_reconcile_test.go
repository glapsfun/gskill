package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_BackfillsIncompleteManifest covers 008 FR-009: a hand-authored
// manifest with only source+path is brought to the always-present form (version
// pin + agent set) by `install`, and a re-run is byte-identical (idempotent).
func TestInstall_BackfillsIncompleteManifest(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	body := "schema_version = 1\n\n[skills.demo]\nsource = \"" + repo + "\"\npath = \"demo\"\n"
	if err := os.WriteFile(filepath.Join(proj, "gskill.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	demo := section(string(readFile(t, filepath.Join(proj, "gskill.toml"))), "[skills.demo]")
	if !strings.Contains(demo, "version = '^1.2.0'") {
		t.Errorf("install did not backfill version pin:\n%s", demo)
	}
	if !strings.Contains(demo, "agents = ['claude']") {
		t.Errorf("install did not backfill agent set:\n%s", demo)
	}

	// Idempotent: a second install changes neither file.
	tomlBefore := readFile(t, filepath.Join(proj, "gskill.toml"))
	lockBefore := readFile(t, filepath.Join(proj, "gskill.lock"))
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("second install: %s", stderr)
	}
	if !bytes.Equal(readFile(t, filepath.Join(proj, "gskill.toml")), tomlBefore) {
		t.Error("manifest rewritten on idempotent re-install")
	}
	if !bytes.Equal(readFile(t, filepath.Join(proj, "gskill.lock")), lockBefore) {
		t.Error("lockfile rewritten on idempotent re-install")
	}
}

// TestSync_BackfillsLegacyManifestFromLock covers 008 FR-009 for the t1-style
// case: a complete lock but an incomplete manifest (no version) is migrated to
// the pinned form by sync using the LOCKED resolution (no re-resolve), and a
// second sync is a byte-identical no-op.
func TestSync_BackfillsLegacyManifestFromLock(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	// Bare add writes a complete lock; then strip the manifest back to source+path
	// to simulate a manifest produced by a pre-008 build.
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	body := "schema_version = 1\n\n[skills.demo]\nsource = \"" + repo + "\"\npath = \"demo\"\n"
	if err := os.WriteFile(filepath.Join(proj, "gskill.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("migrating sync: %s", stderr)
	}
	demo := section(string(readFile(t, filepath.Join(proj, "gskill.toml"))), "[skills.demo]")
	if !strings.Contains(demo, "version = '^1.2.0'") || !strings.Contains(demo, "agents = ['claude']") {
		t.Errorf("sync did not migrate legacy manifest:\n%s", demo)
	}

	// Second sync is a byte-identical no-op.
	tomlBefore := readFile(t, filepath.Join(proj, "gskill.toml"))
	lockBefore := readFile(t, filepath.Join(proj, "gskill.lock"))
	stdout, stderr, code := runGskill(t, proj, "--json", "sync")
	if code != 0 {
		t.Fatalf("second sync: %s", stderr)
	}
	if !strings.Contains(stdout, `"up_to_date": true`) {
		t.Errorf("second sync not up to date:\n%s", stdout)
	}
	if !bytes.Equal(readFile(t, filepath.Join(proj, "gskill.toml")), tomlBefore) {
		t.Error("manifest rewritten on idempotent re-sync")
	}
	if !bytes.Equal(readFile(t, filepath.Join(proj, "gskill.lock")), lockBefore) {
		t.Error("lockfile rewritten on idempotent re-sync")
	}
}

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
