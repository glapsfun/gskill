package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifestIntent_AgentsAreHonored: the manifest is documented as the intent
// half (FR-001), so a declared agent set must actually drive installation.
// Writing `agents` into every generated block while install ignores it would
// make three-quarters of the file decorative.
func TestManifestIntent_AgentsAreHonored(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Declare a second agent by hand, as a user would.
	setManifestKey(t, proj, "demo", "agents", "")
	data, err := os.ReadFile(manifestPath(proj))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(data), `agents = ""`, `agents = ["claude", "codex"]`, 1)
	if err := os.WriteFile(manifestPath(proj), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}

	if _, err := os.Stat(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Errorf("declared agent was ignored by install: %v", err)
	}
}

// TestManifestIntent_ModeIsNotFabricated: `mode` records what the user asked
// for. Writing the *resolved* mode instead would commit "symlink" as if it were
// intent just because the machine that ran add happened to support symlinks,
// and every teammate would inherit a choice nobody made.
func TestManifestIntent_ModeIsNotFabricated(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	if got := readManifest(t, proj); strings.Contains(got, "mode =") {
		t.Errorf("add invented a mode the user never declared:\n%s", got)
	}
}

// TestManifestIntent_ExplicitModeIsRecorded: when the user does ask for a mode,
// it is intent and belongs in the manifest.
func TestManifestIntent_ExplicitModeIsRecorded(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--copy"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	if got := readManifest(t, proj); !strings.Contains(got, `mode = "copy"`) {
		t.Errorf("explicitly requested mode was not recorded:\n%s", got)
	}
}

// TestManifestIntent_NarrowingRemovesTargets: narrowing the declared agent set
// must remove the dropped agent's target, not just its lock record. Leaving the
// directory behind produces an orphan no later command tracks — the agent keeps
// reading a skill gskill believes it uninstalled.
func TestManifestIntent_NarrowingRemovesTargets(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude,codex"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Fatalf("codex target missing after add: %v", err)
	}

	// Narrow the declaration to claude alone.
	data, err := os.ReadFile(manifestPath(proj))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(data), `agents = ["claude", "codex"]`, `agents = ["claude"]`, 1)
	if err := os.WriteFile(manifestPath(proj), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(proj, ".codex", "skills", "demo")); err == nil {
		t.Error("dropped agent's target survived as an untracked orphan")
	}
}
