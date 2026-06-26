package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrozenMismatch_Exit4AndZeroAgentDirsModified(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Remove the installed skill so we can prove a frozen mismatch installs nothing.
	installed := filepath.Join(proj, ".claude", "skills", "demo")
	if err := os.RemoveAll(installed); err != nil {
		t.Fatal(err)
	}

	// Hand-edit the manifest so it disagrees with the lock.
	manifestPath := filepath.Join(proj, "gskill.toml")
	manifestData := string(readFile(t, manifestPath))
	manifestData = strings.Replace(manifestData, "^1.0.0", "^9.0.0", 1)
	if err := os.WriteFile(manifestPath, []byte(manifestData), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, code := runGskill(t, proj, "install", "--frozen-lockfile")
	if code != 4 {
		t.Errorf("exit code = %d, want 4 (lockfile mismatch)", code)
	}
	if _, err := os.Stat(filepath.Join(installed, "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("agent dir was modified on a frozen mismatch (stat err=%v)", err)
	}
}
