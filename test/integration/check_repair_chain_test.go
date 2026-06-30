package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addShared installs the demo skill for claude+codex and returns the project.
func addShared(t *testing.T) (proj, repo string) {
	t.Helper()
	repo = gitRepo(t, validSkill("demo"), "v1.0.0")
	proj = newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude"); code != 0 {
		t.Fatalf("add claude: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "codex"); code != 0 {
		t.Fatalf("add codex: %s", stderr)
	}
	return proj, repo
}

// TestCheckChain_HealthyThenDrift covers US3 scenarios 1–2: a healthy chain
// passes; a deleted agent target is reported as drift (exit 7 with the gate).
func TestCheckChain_HealthyThenDrift(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if _, stderr, code := runGskill(t, proj, "check", "--fail-on-drift"); code != 0 {
		t.Fatalf("healthy check --fail-on-drift exit %d: %s", code, stderr)
	}

	// Delete one agent target.
	if err := os.RemoveAll(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runGskill(t, proj, "--json", "check", "--fail-on-drift")
	if code != 7 {
		t.Errorf("drift check exit = %d, want 7", code)
	}
	if !strings.Contains(stdout, `"has_drift": true`) {
		t.Errorf("check did not report drift:\n%s", stdout)
	}
}

// TestCheckChain_BrokenActiveLinkDetected covers US3/SC-005: a broken active
// entry (dangling agent symlinks) is detected as drift.
func TestCheckChain_BrokenActiveLinkDetected(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	// Remove the active layer out from under the agent symlinks.
	if err := os.RemoveAll(filepath.Join(proj, ".agents")); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runGskill(t, proj, "check", "--fail-on-drift"); code != 7 {
		t.Errorf("broken active link not detected as drift (exit %d, want 7)", code)
	}
}

// TestCheckChain_CorruptStoreFailsClosed covers US3 scenario 4 / FR-018: corrupt
// store content fails closed under verify (exit 6).
func TestCheckChain_CorruptStoreFailsClosed(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	// Tamper with the stored content.
	storePath, err := filepath.EvalSymlinks(filepath.Join(proj, ".agents", "skills", "demo"))
	if err != nil {
		t.Fatalf("eval store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storePath, "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, _, code := runGskill(t, proj, "verify"); code != 6 {
		t.Errorf("verify on corrupt content exit = %d, want 6", code)
	}
}

// TestRepairChain_RecreatesTargetThroughActive covers US3 scenario 3 / SC-006:
// repair recreates a deleted target through the active entry, and a follow-up
// check passes.
func TestRepairChain_RecreatesTargetThroughActive(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if err := os.RemoveAll(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskill(t, proj, "repair"); code != 0 {
		t.Fatalf("repair: %s", stderr)
	}
	requireResolvesActive(t, proj, ".codex", "demo")
	if _, stderr, code := runGskill(t, proj, "check", "--fail-on-drift"); code != 0 {
		t.Errorf("check after repair exit %d: %s", code, stderr)
	}
}

// TestRepairChain_RepointsBrokenActive covers US3/R9-adjacent: repair recreates a
// missing active entry and the agent targets follow.
func TestRepairChain_RepointsBrokenActive(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if err := os.RemoveAll(filepath.Join(proj, ".agents")); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskill(t, proj, "repair"); code != 0 {
		t.Fatalf("repair: %s", stderr)
	}
	if n := countActiveEntries(t, proj); n != 1 {
		t.Errorf("active entry not recreated (count=%d)", n)
	}
	requireResolvesActive(t, proj, ".claude", "demo")
	requireResolvesActive(t, proj, ".codex", "demo")
}
