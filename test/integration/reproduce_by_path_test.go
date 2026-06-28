package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// gitRepoAt creates a git repo with a skill at an arbitrary in-repo subpath
// (forward-slash) and returns the repo path. Unlike gitRepo, the skill is not
// at folder depth 1, so a restore must rely on the recorded in-repo path.
func gitRepoAt(t *testing.T, subpath, name string, tags ...string) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "--quiet", "-b", "main")
	dir := filepath.Join(repo, filepath.FromSlash(subpath))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(validSkill(name)), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "--quiet", "-m", "initial")
	for _, tag := range tags {
		gitRun(t, repo, "tag", tag)
	}
	return repo
}

// TestReproduceByPath proves SC-006/FR-030: a skill installed from a deep
// in-repo path is reproduced byte-identically on a clean checkout using only the
// recorded (commit, in-repo path) — the frozen restore relocates it without
// re-running discovery heuristics.
func TestReproduceByPath_DeepPath(t *testing.T) {
	t.Parallel()

	repo := gitRepoAt(t, "skills/category/deep-skill", "deep-skill", "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Manifest/lock pin the in-repo path.
	manifest := string(readFile(t, filepath.Join(proj, "gskill.toml")))
	if !bytes.Contains([]byte(manifest), []byte("skills/category/deep-skill")) {
		t.Errorf("manifest must record the deep in-repo path:\n%s", manifest)
	}

	installedBefore := readFile(t, filepath.Join(proj, ".claude", "skills", "deep-skill", "SKILL.md"))
	lockBefore := readFile(t, filepath.Join(proj, "gskill.lock"))

	// Clean checkout: drop state + installed content.
	if err := os.RemoveAll(filepath.Join(proj, ".gskill")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile"); code != 0 {
		t.Fatalf("frozen restore exit: %s", stderr)
	}
	if lockAfter := readFile(t, filepath.Join(proj, "gskill.lock")); !bytes.Equal(lockBefore, lockAfter) {
		t.Error("frozen restore modified the lockfile")
	}
	installedAfter := readFile(t, filepath.Join(proj, ".claude", "skills", "deep-skill", "SKILL.md"))
	if !bytes.Equal(installedBefore, installedAfter) {
		t.Error("restored content differs from original (path-based reproduction failed)")
	}
}
