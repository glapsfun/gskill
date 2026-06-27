package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

// Not parallel: sets HOME to redirect the user-global location.
func TestGlobalScope_InstallsToUserGlobalLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--global", "--agent", "claude-code"); code != 0 {
		t.Fatalf("add --global: %s", stderr)
	}

	globalPath := filepath.Join(home, ".claude", "skills", "demo", "SKILL.md")
	if _, err := os.Stat(globalPath); err != nil {
		t.Errorf("skill not installed at user-global location %s: %v", globalPath, err)
	}
}
