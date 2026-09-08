package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCloneAndGo_OverriddenSkill is spec 023 FR-017 — 022's clone-and-go
// promise extended to overrides. A teammate cloning the repository must get the
// *customized* skill through the agent path with zero gskill commands, and a
// subsequent frozen install must accept what is committed rather than fail
// closed against a hash of un-customized content.
func TestCloneAndGo_OverriddenSkill(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	rules := filepath.Join(proj, "gskill", "demo", "rules.md")
	if err := os.MkdirAll(filepath.Dir(rules), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules, []byte("## House rules\nCite file:line.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declareOverride(t, proj, "demo", "append = [\"gskill/demo/rules.md\"]\n")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}

	// Commit everything a teammate would receive.
	gitRun(t, proj, "init", "--quiet")
	gitRun(t, proj, "add", "-A")
	gitRun(t, proj, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "--quiet", "-m", "skills")

	clone := filepath.Join(t.TempDir(), "clone")
	cloneCmd := exec.CommandContext(t.Context(), "git", "clone", "--quiet", proj, clone) //nolint:gosec // both paths are test-owned temp dirs
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v: %s", err, out)
	}

	// Zero gskill commands: the agent path must already read the customization.
	content, err := os.ReadFile(filepath.Join(clone, ".claude", "skills", "demo", "SKILL.md")) //nolint:gosec // test clone
	if err != nil {
		t.Fatalf("skill unreadable through the agent path in a fresh clone: %v", err)
	}
	if !strings.Contains(string(content), "House rules") {
		t.Errorf("clone did not receive the customized content:\n%s", content)
	}

	// A frozen install must accept the committed result. Before the shared
	// computedHash was taken from the shipped content, this failed with exit 6
	// because the lock described the un-overridden upstream.
	if _, stderr, code := runGskill(t, clone, "install", "--frozen-lockfile"); code != 0 {
		t.Errorf("frozen install on a fresh clone failed: exit %d: %s", code, stderr)
	}
}

// TestCloneAndGo_OverriddenRestoreFromCache is the sync/repair path: with the
// committed content deleted, re-materializing must re-apply the override
// rather than hashing bare upstream and failing its own restore.
func TestCloneAndGo_OverriddenRestoreFromCache(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	rules := filepath.Join(proj, "gskill", "demo", "rules.md")
	if err := os.MkdirAll(filepath.Dir(rules), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules, []byte("## House rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declareOverride(t, proj, "demo", "append = [\"gskill/demo/rules.md\"]\n")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}

	// Simulate a checkout that lost its committed content.
	if err := os.RemoveAll(filepath.Join(proj, ".agents", "skills", "demo")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "project", "sync"); code != 0 {
		t.Fatalf("sync after losing committed content: exit %d: %s", code, stderr)
	}
	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")) //nolint:gosec // test project
	if err != nil {
		t.Fatalf("content not restored: %v", err)
	}
	if !strings.Contains(string(content), "House rules") {
		t.Errorf("restore dropped the override:\n%s", content)
	}
}
