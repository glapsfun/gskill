package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFrozenRestore_CleanCheckoutMatchesLockAndIsByteIdentical(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))
	installedBefore := readFile(t, filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md"))

	// Simulate a clean checkout: drop the state dir and installed content.
	if err := os.RemoveAll(filepath.Join(proj, ".gskill")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile"); code != 0 {
		t.Fatalf("frozen restore exit: %s", stderr)
	}

	// Lock is not modified by a frozen restore (SC-002).
	if lockAfter := readFile(t, filepath.Join(proj, "skills-lock.json")); !bytes.Equal(lockBefore, lockAfter) {
		t.Errorf("frozen restore modified the lockfile")
	}
	// Installed content matches the original byte-for-byte (SC-001).
	installedAfter := readFile(t, filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md"))
	if !bytes.Equal(installedBefore, installedAfter) {
		t.Errorf("restored content differs from original")
	}
}
