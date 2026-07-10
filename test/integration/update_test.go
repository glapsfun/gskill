package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdate_AdvancesLockWithinConstraint(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.0.0"`) {
		t.Fatalf("initial lock not at 1.0.0:\n%s", lock)
	}

	// Publish a newer in-constraint version on a new commit.
	if err := os.WriteFile(filepath.Join(repo, "demo", "SKILL.md"),
		[]byte("---\nname: demo\ndescription: updated demo\n---\n# demo v1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "--quiet", "-m", "v1.1")
	gitRun(t, repo, "tag", "v1.1.0")

	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}

	lock = string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.1.0"`) {
		t.Errorf("lock did not advance to 1.1.0:\n%s", lock)
	}
}
