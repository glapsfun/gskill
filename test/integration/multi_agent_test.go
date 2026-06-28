package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiAgent_OneAddInstallsIntoAllDetected(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")

	proj := t.TempDir()
	for _, marker := range []string{".claude", ".codex"} {
		if err := os.MkdirAll(filepath.Join(proj, marker), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	for _, marker := range []string{".claude", ".codex"} {
		path := filepath.Join(proj, marker, "skills", "demo", "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("skill not installed into %s: %v", marker, err)
		}
	}

	lock := string(readFile(t, filepath.Join(proj, "gskill.lock")))
	for _, id := range []string{"claude", "codex"} {
		if !strings.Contains(lock, id) {
			t.Errorf("lock targets missing %q:\n%s", id, lock)
		}
	}
}
