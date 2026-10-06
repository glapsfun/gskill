package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mismatchedSkillTree creates a source tree holding skills/<folder> whose
// SKILL.md declares a different name. gskill installs it under the folder name
// (folder identity), but OpenCode will not load it.
func mismatchedSkillTree(t *testing.T, folder, declared string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "skills", folder)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(validSkill(declared)), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestOpenCode_DetectedFromItsMarker(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	if err := os.MkdirAll(filepath.Join(proj, ".opencode"), 0o750); err != nil {
		t.Fatal(err)
	}
	initProject(t, proj)
	before := topLevel(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo")); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	requireOnlyGskillEntries(t, proj, before)
	if _, err := os.Lstat(filepath.Join(proj, ".opencode", "skills")); !os.IsNotExist(err) {
		t.Errorf("OpenCode got a per-agent skills dir instead of reading the store: %v", err)
	}
	if got := readSharedLock(t, proj).Skills["demo"].Gskill.State.Modes["opencode"]; got != modeShared {
		t.Errorf("opencode mode = %q, want shared (detected target)", got)
	}
}

func TestOpenCode_NamingProblemWarnsWithoutFailing(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	_, stderr, code := runGskill(t, proj, "add", mismatchedSkillTree(t, "demo", "other"), "--all", "--agent", "opencode")
	if code != 0 {
		t.Fatalf("add = %d, want 0: %s", code, stderr)
	}
	const want = `opencode: skill "demo" declares name "other"; OpenCode requires it to match the directory`
	if n := strings.Count(stderr, want); n != 1 {
		t.Errorf("advisory printed %d times, want once:\n%s", n, stderr)
	}

	stdout, stderr, code := runGskill(t, proj, "--json", "project", "check", "--fail-on-drift")
	if code != 0 {
		t.Fatalf("check = %d, want 0 (advisories never fail): %s", code, stderr)
	}
	var res struct {
		HasDrift   bool     `json:"has_drift"`
		Advisories []string `json:"advisories"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse check --json: %v\n%s", err, stdout)
	}
	if res.HasDrift || len(res.Advisories) != 1 || res.Advisories[0] != want {
		t.Errorf("check = %+v, want no drift and the one advisory", res)
	}
}

// Not parallel: sets HOME to redirect the user-global location.
func TestOpenCode_GlobalInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--global", "--agent", "opencode"); code != 0 {
		t.Fatalf("add --global --agent opencode: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("global copy missing: %v", err)
	}
}

// TestOpenCode_AdviceWhenAddingAnotherAgent covers an agent-add that only
// touches claude: the OpenCode advice still comes from the skill's full set of
// locked targets.
func TestOpenCode_AdviceWhenAddingAnotherAgent(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	tree := mismatchedSkillTree(t, "demo", "other")
	if _, stderr, code := runGskill(t, proj, "add", tree, "--all", "--agent", "opencode"); code != 0 {
		t.Fatalf("add --agent opencode: %s", stderr)
	}
	_, stderr, code := runGskill(t, proj, "add", tree, "--all", "--agent", "claude")
	if code != 0 {
		t.Fatalf("add --agent claude: %s", stderr)
	}
	const want = `opencode: skill "demo" declares name "other"; OpenCode requires it to match the directory`
	if n := strings.Count(stderr, want); n != 1 {
		t.Errorf("OpenCode advice printed %d times, want once:\n%s", n, stderr)
	}
}
