package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenClaw_ProjectInstallUsesTheStore(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	before := topLevel(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--agent", "openclaw"); code != 0 {
		t.Fatalf("add --agent openclaw: %s", stderr)
	}
	requireOnlyGskillEntries(t, proj, before)
	requireStoreContent(t, proj, "demo")
}

// Not parallel: sets HOME to redirect the user-global location.
func TestOpenClaw_GlobalInstallIsARealCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--global", "--agent", "openclaw"); code != 0 {
		t.Fatalf("add --global --agent openclaw: %s", stderr)
	}
	// OpenClaw rejects skill roots whose real path escapes its skills dir, so
	// the global install must be a real directory, never a symlink.
	info, err := os.Lstat(filepath.Join(home, ".openclaw", "skills", "demo"))
	if err != nil {
		t.Fatalf("global copy missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Errorf("global install is not a real directory: mode %v", info.Mode())
	}
}

// Not parallel: sets HOME to redirect the user-global location.
func TestOpenClaw_ForeignGlobalSkillIsLeftAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	foreign := filepath.Join(home, ".openclaw", "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o750); err != nil {
		t.Fatal(err)
	}
	mine := []byte("# installed by ClawHub, not gskill\n")
	if err := os.WriteFile(foreign, mine, 0o600); err != nil {
		t.Fatal(err)
	}

	proj := newProject(t)
	initProject(t, proj)
	_, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--global", "--agent", "openclaw")
	if code == 0 {
		t.Fatal("add over an unmanaged global skill succeeded, want refusal")
	}
	if !bytes.Contains([]byte(stderr), []byte("not managed by gskill")) {
		t.Errorf("refusal does not explain the foreign content:\n%s", stderr)
	}
	if got := readFile(t, foreign); !bytes.Equal(got, mine) {
		t.Errorf("foreign skill was modified:\n%s", got)
	}
}
