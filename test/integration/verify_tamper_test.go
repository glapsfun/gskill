package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerify_SingleByteTamperIsExit6(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Clean verify passes.
	if _, stderr, code := runGskill(t, proj, "verify"); code != 0 {
		t.Fatalf("clean verify failed: %s", stderr)
	}

	// Tamper one installed byte (writes through the symlink into the store).
	target := filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")
	data := readFile(t, target)
	if err := os.WriteFile(target, append(data, '!'), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, code := runGskill(t, proj, "verify")
	if code != 6 {
		t.Errorf("exit code = %d, want 6 (integrity failure)", code)
	}
}
