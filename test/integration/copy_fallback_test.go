package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyMode_RecordedAndMaterializedAsRealDir(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	// --copy forces the same activation path used by the symlink-unsupported
	// fallback, which records mode: copy (FR-020).
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--copy"); code != 0 {
		t.Fatalf("add --copy: %s", stderr)
	}

	lock := string(readFile(t, filepath.Join(proj, "gskill.lock")))
	if !strings.Contains(lock, `"mode": "copy"`) {
		t.Errorf("lock did not record mode: copy:\n%s", lock)
	}

	// The installed target is a real directory, not a symlink.
	info, err := os.Lstat(filepath.Join(proj, ".claude", "skills", "demo"))
	if err != nil {
		t.Fatalf("lstat target: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("target is a symlink; --copy should produce a real directory")
	}
}
