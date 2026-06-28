package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurity_UnsafeSymlinkRejected(t *testing.T) {
	t.Parallel()

	// Local skill whose content contains a symlink escaping the skill dir.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(validSkill("demo")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "escape")); err != nil {
		t.Skip("symlinks unsupported")
	}

	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}

	_, stderr, code := runGskill(t, proj, "add", src, "--agent", "claude")
	if code == 0 {
		t.Fatal("add of a skill with an escaping symlink succeeded, want rejection")
	}
	if !strings.Contains(strings.ToLower(stderr), "symlink") {
		t.Errorf("rejection should mention the unsafe symlink: %q", stderr)
	}
}

func TestSecurity_ExecBitWarnsButInstalls(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(validSkill("demo")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o700); err != nil { //nolint:gosec // exercising exec-bit detection
		t.Fatal(err)
	}

	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}

	_, stderr, code := runGskill(t, proj, "add", src, "--agent", "claude")
	if code != 0 {
		t.Fatalf("add with exec-bit file failed: %s", stderr)
	}
	if !strings.Contains(strings.ToLower(stderr), "executable") {
		t.Errorf("expected an executable-bit warning: %q", stderr)
	}
}
