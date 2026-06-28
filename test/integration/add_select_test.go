package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manySource creates a local source with several skills under skills/<name>.
func manySource(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		dir := filepath.Join(root, "skills", n)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(validSkill(n)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func installedDirs(t *testing.T, proj string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(proj, ".claude", "skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestAddSelect_OneByName(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing", "kubernetes-ops")
	proj := newProject(t)
	mustInit(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", src, "--skill", "code-review"); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	if got := installedDirs(t, proj); len(got) != 1 || got[0] != "code-review" {
		t.Errorf("installed = %v, want [code-review]", got)
	}
}

func TestAddSelect_SeveralByRepeatedName(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing", "kubernetes-ops")
	proj := newProject(t)
	mustInit(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", src, "--skill", "code-review", "--skill", "writing"); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	got := installedDirs(t, proj)
	if len(got) != 2 {
		t.Errorf("installed = %v, want 2 skills", got)
	}
}

func TestAddSelect_AllWildcard(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing", "kubernetes-ops")
	proj := newProject(t)
	mustInit(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", src, "--skill", "*"); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	if got := installedDirs(t, proj); len(got) != 3 {
		t.Errorf("installed = %v, want 3 skills", got)
	}
}

func TestAddSelect_ListOnlyInstallsNothing(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)
	mustInit(t, proj)

	stdout, stderr, code := runGskill(t, proj, "--json", "add", src, "--list")
	if code != 0 {
		t.Fatalf("add --list exit %d: %s", code, stderr)
	}
	var listed []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("list json: %v\n%s", err, stdout)
	}
	if len(listed) != 2 {
		t.Errorf("listed %d skills, want 2", len(listed))
	}
	if got := installedDirs(t, proj); len(got) != 0 {
		t.Errorf("--list must not install, got %v", got)
	}
}

func TestAddSelect_NoSelectorNonInteractiveFails(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)
	mustInit(t, proj)

	_, stderr, code := runGskill(t, proj, "--no-interactive", "add", src)
	if code != 2 {
		t.Errorf("exit = %d, want 2 (needs a selector)", code)
	}
	if !strings.Contains(strings.ToLower(stderr), "skill") {
		t.Errorf("error should guide to --skill: %q", stderr)
	}
	if got := installedDirs(t, proj); len(got) != 0 {
		t.Errorf("nothing should be installed, got %v", got)
	}
}

func TestAddSelect_NoMatchSuggests(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)
	mustInit(t, proj)

	_, stderr, code := runGskill(t, proj, "add", src, "--skill", "code-revoew")
	if code != 2 {
		t.Errorf("exit = %d, want 2 (no match)", code)
	}
	if !strings.Contains(stderr, "code-review") {
		t.Errorf("error should suggest code-review: %q", stderr)
	}
	if got := installedDirs(t, proj); len(got) != 0 {
		t.Errorf("nothing should be installed, got %v", got)
	}
}

// TestAddSelect_AtomicOnCollision proves FR-046: when one selected skill
// collides with an existing manifest key, the whole batch fails and the new
// skill is not left installed.
func TestAddSelect_AtomicOnCollision(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)
	mustInit(t, proj)

	// Pre-install code-review.
	if _, stderr, code := runGskill(t, proj, "add", src, "--skill", "code-review"); code != 0 {
		t.Fatalf("first add: %s", stderr)
	}
	manifestBefore := readFile(t, filepath.Join(proj, "gskill.toml"))

	// Now add writing + code-review together: code-review collides (no --force).
	_, _, code := runGskill(t, proj, "add", src, "--skill", "writing", "--skill", "code-review")
	if code == 0 {
		t.Fatal("batch with a colliding skill should fail")
	}
	// Atomic: writing must NOT be installed, manifest unchanged.
	dirs := installedDirs(t, proj)
	for _, d := range dirs {
		if d == "writing" {
			t.Error("writing was installed despite the batch failing (not atomic)")
		}
	}
	if manifestAfter := readFile(t, filepath.Join(proj, "gskill.toml")); string(manifestAfter) != string(manifestBefore) {
		t.Error("manifest changed despite the failed batch")
	}
}

// mustInit runs `gskill init`, failing the test on error.
func mustInit(t *testing.T, proj string) {
	t.Helper()
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
}

// TestAddSelect_AgentTargetingUnattended covers FR-023: explicit --agent installs
// each selected skill into exactly the named agents, and the run never blocks.
func TestAddSelect_AgentTargetingUnattended(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")

	// Project with both agent markers present.
	proj := t.TempDir()
	for _, marker := range []string{".claude", ".codex"} {
		if err := os.MkdirAll(filepath.Join(proj, marker), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	mustInit(t, proj)

	// Select two skills, target only codex, unattended.
	_, stderr, code := runGskill(t, proj, "--no-interactive", "add", src,
		"--skill", "code-review", "--skill", "writing", "--agent", "codex", "--yes")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	for _, name := range []string{"code-review", "writing"} {
		if _, err := os.Stat(filepath.Join(proj, ".codex", "skills", name, "SKILL.md")); err != nil {
			t.Errorf("%s not installed into codex: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", name)); err == nil {
			t.Errorf("%s should not be installed into claude (not targeted)", name)
		}
	}
}

// TestAddSelect_ListWithoutAgents is a regression test: --list is read-only and
// must succeed even when no target agent is detected.
func TestAddSelect_ListWithoutAgents(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := t.TempDir() // no agent markers
	mustInit(t, proj)

	stdout, stderr, code := runGskill(t, proj, "--json", "add", src, "--list")
	if code != 0 {
		t.Fatalf("add --list with no agents should succeed, exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "code-review") {
		t.Errorf("list output missing skills: %s", stdout)
	}
}
