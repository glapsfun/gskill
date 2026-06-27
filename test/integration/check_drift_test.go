package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheck_FailOnDriftGating(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Clean install: --fail-on-drift exits 0.
	if _, stderr, code := runGskill(t, proj, "check", "--fail-on-drift"); code != 0 {
		t.Fatalf("clean check --fail-on-drift exit %d: %s", code, stderr)
	}

	// Induce drift by removing the installed skill.
	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills", "demo")); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runGskill(t, proj, "check", "--fail-on-drift"); code != 7 {
		t.Errorf("drift exit code = %d, want 7", code)
	}
	// Without the flag, check reports drift but exits 0.
	if _, _, code := runGskill(t, proj, "check"); code != 0 {
		t.Errorf("check without --fail-on-drift exit = %d, want 0", code)
	}
}
