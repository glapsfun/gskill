package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatus_ReportsAgentsAndHealth covers US4 scenario 1 / FR-021: status lists
// each skill's source, identity, active health, and per-agent mode + health.
func TestStatus_ReportsAgentsAndHealth(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	stdout, stderr, code := runGskill(t, proj, "--json", "status")
	if code != 0 {
		t.Fatalf("status exit %d: %s", code, stderr)
	}
	for _, want := range []string{`"name": "demo"`, `"active": "ok"`, `"id": "claude"`, `"id": "codex"`, `"health": "ok-symlink"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status JSON missing %q:\n%s", want, stdout)
		}
	}
}

// TestStatus_ExitsZeroOnDrift covers the contract: status is informational and
// exits 0 even when drift exists (contrast with check).
func TestStatus_ExitsZeroOnDrift(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if err := os.RemoveAll(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runGskill(t, proj, "--json", "status")
	if code != 0 {
		t.Errorf("status on drift exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"health": "missing"`) {
		t.Errorf("status did not surface the missing target:\n%s", stdout)
	}
}

// TestUnlink_OneAgentKeepsOthers covers US4 scenario 2 / SC-008: unlinking one
// agent removes only its target and keeps shared content and the other agent.
func TestUnlink_OneAgentKeepsOthers(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if _, stderr, code := runGskill(t, proj, "unlink", "demo", "--agent", "codex"); code != 0 {
		t.Fatalf("unlink: %s", stderr)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".codex", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("codex target not removed (err=%v)", err)
	}
	requireResolvesActive(t, proj, ".claude", "demo") // kept
	if n := countActiveEntries(t, proj); n != 1 {
		t.Errorf("active entry count = %d, want 1 (retained)", n)
	}
	requireManifestAgents(t, proj, "claude")
	man := string(readFile(t, filepath.Join(proj, "gskill.toml")))
	if strings.Contains(man, "codex") {
		t.Errorf("manifest still lists codex after unlink:\n%s", man)
	}
}

// TestUnlink_LastAgentRetainsUnlessPrune covers US4 scenario 3 / Q3: unlinking
// the last agent retains the active entry, store, and manifest until --prune.
func TestUnlink_LastAgentRetainsUnlessPrune(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Unlink the only agent without --prune: skill retained, unreferenced.
	stdout, stderr, code := runGskill(t, proj, "--json", "unlink", "demo", "--agent", "claude")
	if code != 0 {
		t.Fatalf("unlink: %s", stderr)
	}
	if !strings.Contains(stdout, `"unreferenced": true`) || !strings.Contains(stdout, `"pruned": false`) {
		t.Errorf("expected retained-but-unreferenced result:\n%s", stdout)
	}
	if n := countActiveEntries(t, proj); n != 1 {
		t.Errorf("active entry not retained (count=%d)", n)
	}
	if man := string(readFile(t, filepath.Join(proj, "gskill.toml"))); !strings.Contains(man, "demo") {
		t.Errorf("manifest entry dropped without --prune:\n%s", man)
	}
	// The store content is retained for an instant re-add.
	if n := countStoreEntries(t, proj); n != 1 {
		t.Errorf("store content not retained (count=%d)", n)
	}
}

// TestUnlink_PrunesLastAgent covers US4 scenario 3 with --prune in a single step.
func TestUnlink_PrunesLastAgent(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	stdout, stderr, code := runGskill(t, proj, "--json", "unlink", "demo", "--agent", "claude", "--prune")
	if code != 0 {
		t.Fatalf("unlink --prune: %s", stderr)
	}
	if !strings.Contains(stdout, `"pruned": true`) {
		t.Errorf("expected pruned result:\n%s", stdout)
	}
	if n := countActiveEntries(t, proj); n != 0 {
		t.Errorf("active entry not pruned (count=%d)", n)
	}
	if man := string(readFile(t, filepath.Join(proj, "gskill.toml"))); strings.Contains(man, "demo") {
		t.Errorf("manifest entry not pruned:\n%s", man)
	}
}

// TestUnlink_UsageErrors covers T038: missing --agent (exit 2), unknown agent
// (exit 9), agent not declared for the skill (exit 3).
func TestUnlink_UsageErrors(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	if _, _, code := runGskill(t, proj, "unlink", "demo"); code != 2 {
		t.Errorf("missing --agent exit = %d, want 2", code)
	}
	if _, _, code := runGskill(t, proj, "unlink", "demo", "--agent", "nope"); code != 9 {
		t.Errorf("unknown agent exit = %d, want 9", code)
	}
	if _, _, code := runGskill(t, proj, "unlink", "demo", "--agent", "gemini-cli"); code != 3 {
		t.Errorf("agent-not-on-skill exit = %d, want 3", code)
	}
}
