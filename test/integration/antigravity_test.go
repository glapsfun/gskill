package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

// gskillOwned is everything a project install may add at the repo root.
var gskillOwned = map[string]bool{
	".agents": true, ".gskill": true, ".gitignore": true,
	"skills.toml": true, "skills-lock.json": true,
}

// requireOnlyGskillEntries fails when root gained any top-level entry outside
// gskill's own files since before, the snapshot taken ahead of the install
// (spec 027 SC-003).
func requireOnlyGskillEntries(t *testing.T, root string, before map[string]bool) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !before[e.Name()] && !gskillOwned[e.Name()] {
			t.Errorf("install created %s outside the shared store", e.Name())
		}
	}
}

func topLevel(t *testing.T, root string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names
}

func TestAntigravity_ProjectInstallUsesTheStore(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	before := topLevel(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--agent", "antigravity"); code != 0 {
		t.Fatalf("add --agent antigravity: %s", stderr)
	}

	requireOnlyGskillEntries(t, proj, before)
	if _, err := os.Lstat(filepath.Join(proj, ".claude", "skills")); !os.IsNotExist(err) {
		t.Errorf("an untargeted agent dir was written: %v", err)
	}
	requireStoreContent(t, proj, "demo")
	state := readSharedLock(t, proj).Skills["demo"].Gskill.State
	if got := state.Targets["antigravity"]; got != ".agents/skills/demo" {
		t.Errorf("antigravity target = %q, want .agents/skills/demo", got)
	}
	if got := state.Modes["antigravity"]; got != modeShared {
		t.Errorf("antigravity mode = %q, want shared", got)
	}

	if _, stderr, code := runGskill(t, proj, "--yes", "remove", "demo"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".agents", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("skill survived remove: %v", err)
	}
}

// Not parallel: sets HOME to redirect the user-global location.
func TestAntigravity_GlobalInstallUsesTheCLIFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--global", "--agent", "antigravity"); code != 0 {
		t.Fatalf("add --global --agent antigravity: %s", stderr)
	}

	dest := filepath.Join(home, ".gemini", "antigravity-cli", "skills", "demo")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("global copy missing at %s: %v", dest, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Errorf("global install is not a real directory: mode %v", info.Mode())
	}
	if _, err := os.Lstat(filepath.Join(home, ".gemini", "config", "skills")); !os.IsNotExist(err) {
		t.Errorf("the folder shared with the Antigravity IDE was written: %v", err)
	}

	if _, stderr, code := runGskill(t, proj, "--yes", "remove", "demo"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Errorf("global copy survived remove: %v", err)
	}
}
