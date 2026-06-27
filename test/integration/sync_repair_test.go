package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncPrune_RemovesOrphans(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Plant an orphan skill directory not present in the lockfile.
	orphan := filepath.Join(proj, ".claude", "skills", "orphan")
	if err := os.MkdirAll(orphan, 0o750); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "sync", "--prune"); code != 0 {
		t.Fatalf("sync --prune: %s", stderr)
	}

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan not pruned (stat err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("declared skill removed by prune: %v", err)
	}
}

func TestRepair_RematerializesBrokenInstall(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Break the install by removing the activated skill directory.
	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills", "demo")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "repair"); code != 0 {
		t.Fatalf("repair: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("repair did not re-materialize the skill: %v", err)
	}
}
