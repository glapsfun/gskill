package integration_test

import (
	"strings"
	"testing"
)

func TestAddConflict_ErrorsAndPointsToUpdateOrForce(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("first add: %s", stderr)
	}

	// Second add of the same skill must fail and point to update / --force.
	_, stderr, code := runGskill(t, proj, "add", repo)
	if code == 0 {
		t.Fatal("second add succeeded, want conflict error")
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3 (invalid manifest)", code)
	}
	low := strings.ToLower(stderr)
	if !strings.Contains(low, "update") && !strings.Contains(low, "force") {
		t.Errorf("conflict message should mention update/--force: %q", stderr)
	}

	// --force overwrites without error.
	if _, stderr, code := runGskill(t, proj, "add", repo, "--force"); code != 0 {
		t.Errorf("add --force failed: %s (code %d)", stderr, code)
	}
}
