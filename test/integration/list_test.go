package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestList_ReportsAgentsAndHealth covers US4 scenario 1 / FR-021: list reports
// each skill's source, identity, active health, and per-agent mode + health.
func TestList_ReportsAgentsAndHealth(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	stdout, stderr, code := runGskill(t, proj, "--json", "list")
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, stderr)
	}
	for _, want := range []string{`"name": "demo"`, `"active": "ok"`, `"id": "claude"`, `"id": "codex"`, `"health": "ok-symlink"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("list JSON missing %q:\n%s", want, stdout)
		}
	}
}

// TestList_ExitsZeroOnDrift covers the contract: list is informational and
// exits 0 even when drift exists (contrast with check).
func TestList_ExitsZeroOnDrift(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if err := os.RemoveAll(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runGskill(t, proj, "--json", "list")
	if code != 0 {
		t.Errorf("list on drift exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"health": "missing"`) {
		t.Errorf("list did not surface the missing target:\n%s", stdout)
	}
}

// TestInstallAgent_NarrowsToExactSet covers spec 021 FR-007: re-running
// install with a narrower exact --agent set detaches the dropped agent —
// its target is removed while shared content, the remaining agent, and the
// lock entry are kept. This is the supported replacement for the retired
// `unlink` command.
func TestInstallAgent_NarrowsToExactSet(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if _, stderr, code := runGskill(t, proj, "install", "--agent", "claude"); code != 0 {
		t.Fatalf("install --agent claude: %s", stderr)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".codex", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("codex target not removed (err=%v)", err)
	}
	requireResolvesActive(t, proj, ".claude", "demo") // kept
	if n := countActiveEntries(t, proj); n != 1 {
		t.Errorf("active entry count = %d, want 1 (retained)", n)
	}
	requireManifestAgents(t, proj, "claude")
	for _, id := range lockAgents(t, proj, "demo") {
		if id == "codex" {
			t.Error("lock still lists codex after narrowing the agent set")
		}
	}
}

// TestRemove_DetachesLastAgent covers spec 021 FR-007: detaching the last
// agent is a removal — `remove` cleans up the target, active entry, and lock
// entry, and no zero-agent, unreferenced lock entry survives. This is the
// supported replacement for the retired `unlink --prune` flow (and the
// retired retained-but-unreferenced state).
func TestRemove_DetachesLastAgent(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	if _, stderr, code := runGskill(t, proj, "--yes", "remove", "demo"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}
	if n := countActiveEntries(t, proj); n != 0 {
		t.Errorf("active entry not removed (count=%d)", n)
	}
	if lock := string(readFile(t, filepath.Join(proj, "skills-lock.json"))); strings.Contains(lock, `"demo"`) {
		t.Errorf("lock entry survived remove (no zero-agent entry may remain):\n%s", lock)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".claude", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("claude target not removed (err=%v)", err)
	}
}
