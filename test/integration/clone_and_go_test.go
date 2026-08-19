package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests prove the repo-owned storage model's core assumption
// (spec 022, US1 / SC-001): a repository with committed skill content at
// .agents/skills/<name> and a committed *relative* agent link yields a
// working skill on a fresh `git clone` with zero gskill commands. They also
// pin the degraded case: a checkout made with core.symlinks=false leaves a
// plain file containing the link text — detectable, never silently repaired
// (macOS/Linux are the only supported platforms; there is no fallback).

const (
	cloneAndGoSkill      = "demo"
	cloneAndGoLinkTarget = "../../.agents/skills/demo"
)

// cloneAndGoFixture builds and commits a repo-owned project by hand (no
// gskill): committed skill content plus a committed relative agent link.
func cloneAndGoFixture(t *testing.T) (fixture, skillBody string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	skillBody = validSkill(cloneAndGoSkill)
	fixture = filepath.Join(t.TempDir(), "src")
	skillDir := filepath.Join(fixture, ".agents", "skills", cloneAndGoSkill)
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillBody), 0o600); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(fixture, ".claude", "skills")
	if err := os.MkdirAll(agentDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cloneAndGoLinkTarget, filepath.Join(agentDir, cloneAndGoSkill)); err != nil {
		t.Fatal(err)
	}
	gitRun(t, fixture, "init", "--quiet", "-b", "main")
	gitRun(t, fixture, "add", ".")
	gitRun(t, fixture, "commit", "--quiet", "-m", "repo-owned skill fixture")
	return fixture, skillBody
}

// TestCloneAndGoFreshClone: fresh clone yields a working skill with zero
// gskill commands.
func TestCloneAndGoFreshClone(t *testing.T) {
	t.Parallel()

	fixture, skillBody := cloneAndGoFixture(t)
	work := t.TempDir()
	gitRun(t, work, "clone", "--quiet", fixture, "clone")
	linkPath := filepath.Join(work, "clone", ".claude", "skills", cloneAndGoSkill)

	fi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("agent path is not a symlink after clone: mode %v", fi.Mode())
	}
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != cloneAndGoLinkTarget || !strings.HasPrefix(got, "../") {
		t.Fatalf("link target = %q, want relative %q", got, cloneAndGoLinkTarget)
	}

	content, err := os.ReadFile(filepath.Join(linkPath, "SKILL.md")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("skill not readable through the agent path: %v", err)
	}
	if string(content) != skillBody {
		t.Fatalf("SKILL.md content mismatch through agent path:\n%s", content)
	}
}

// TestCloneAndGoSymlinkless: a core.symlinks=false checkout is detectable as
// a plain file containing the link text.
func TestCloneAndGoSymlinkless(t *testing.T) {
	t.Parallel()

	fixture, _ := cloneAndGoFixture(t)
	work := t.TempDir()
	gitRun(t, work, "-c", "core.symlinks=false", "clone", "--quiet", fixture, "clone")
	linkPath := filepath.Join(work, "clone", ".claude", "skills", cloneAndGoSkill)

	fi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected a plain file in a symlink-less checkout, got a symlink")
	}
	if !fi.Mode().IsRegular() {
		t.Fatalf("expected a regular file, got mode %v", fi.Mode())
	}
	content, err := os.ReadFile(linkPath) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != cloneAndGoLinkTarget {
		t.Fatalf("degraded checkout artifact = %q, want the link text %q", content, cloneAndGoLinkTarget)
	}
}
