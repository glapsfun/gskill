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

	skillsDir := filepath.Join(proj, ".claude", "skills")

	// A gskill-managed orphan: a symlink into the store no longer in the lock.
	// It mirrors how gskill activates installs, so prune must remove it.
	storeTarget, err := os.Readlink(filepath.Join(skillsDir, "demo"))
	if err != nil {
		t.Fatalf("read demo install target: %v", err)
	}
	managedOrphan := filepath.Join(skillsDir, "ghost")
	if err := os.Symlink(storeTarget, managedOrphan); err != nil {
		t.Fatal(err)
	}

	// Foreign content gskill never placed: a hand-installed skill directory.
	// Prune must NOT delete it.
	foreign := filepath.Join(skillsDir, "handmade")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "sync", "--prune"); code != 0 {
		t.Fatalf("sync --prune: %s", stderr)
	}

	if _, err := os.Lstat(managedOrphan); !os.IsNotExist(err) {
		t.Errorf("managed orphan not pruned (lstat err=%v)", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("foreign skill deleted by prune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "demo", "SKILL.md")); err != nil {
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
