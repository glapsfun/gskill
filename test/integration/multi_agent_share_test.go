package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countStoreEntries returns the number of content-addressed store entries
// (.gskill/store/<algo>/<hash>).
func countStoreEntries(t *testing.T, proj string) int {
	t.Helper()
	base := filepath.Join(proj, ".gskill", "store")
	algos, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read store: %v", err)
	}
	n := 0
	for _, algo := range algos {
		if !algo.IsDir() {
			continue
		}
		hashes, err := os.ReadDir(filepath.Join(base, algo.Name()))
		if err != nil {
			t.Fatalf("read store algo: %v", err)
		}
		n += len(hashes)
	}
	return n
}

// countActiveEntries returns the number of entries under .agents/skills.
func countActiveEntries(t *testing.T, proj string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(proj, ".agents", "skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read active: %v", err)
	}
	return len(entries)
}

// requireResolvesActive fails unless the agent target for name under marker is a
// symlink resolving through the project's active entry.
func requireResolvesActive(t *testing.T, proj, marker, name string) {
	t.Helper()
	target := filepath.Join(proj, marker, "skills", name)
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat %s: %v", target, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s target is not a symlink (want one resolving through the active entry)", marker)
	}
	link, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("readlink %s: %v", target, err)
	}
	if !strings.Contains(link, filepath.Join(".agents", "skills", name)) {
		t.Errorf("%s target links to %q, want the active entry", marker, link)
	}
}

// requireCounts fails unless the project has exactly the given store and active
// entry counts.
func requireCounts(t *testing.T, proj string, store, active int) {
	t.Helper()
	if n := countStoreEntries(t, proj); n != store {
		t.Errorf("store entries = %d, want %d", n, store)
	}
	if n := countActiveEntries(t, proj); n != active {
		t.Errorf("active entries = %d, want %d", n, active)
	}
}

// requireManifestAgents fails unless the manifest mentions every given agent id.
func requireManifestAgents(t *testing.T, proj string, ids ...string) {
	t.Helper()
	got := lockAgents(t, proj, "demo")
	set := map[string]bool{}
	for _, id := range got {
		set[id] = true
	}
	for _, id := range ids {
		if !set[id] {
			t.Errorf("lock missing agent %q (got %v)", id, got)
		}
	}
}

// TestMultiAgentShare_InstallOnceAddSecondAgent covers US1 scenarios 1–3 and
// SC-001/SC-002: a skill installed for one agent is shared with a second agent
// with no duplicate store/active entries and only the new agent target added.
func TestMultiAgentShare_InstallOnceAddSecondAgent(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}

	// Scenario 1: install for claude only.
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude"); code != 0 {
		t.Fatalf("add claude: %s", stderr)
	}
	requireCounts(t, proj, 1, 1)
	requireResolvesActive(t, proj, ".claude", "demo")
	if lock := string(readFile(t, filepath.Join(proj, "skills-lock.json"))); !strings.Contains(lock, ".agents/skills/demo") {
		t.Errorf("lockfile missing active_path:\n%s", lock)
	}

	// Scenario 2: add the same skill for codex — reuse store + active, add only
	// the new agent target.
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "codex"); code != 0 {
		t.Fatalf("add codex: %s", stderr)
	}
	requireCounts(t, proj, 1, 1) // no duplicate download or active dir
	requireResolvesActive(t, proj, ".codex", "demo")
	requireResolvesActive(t, proj, ".claude", "demo") // undisturbed
	requireManifestAgents(t, proj, "claude", "codex")
}

// TestMultiAgentShare_DefaultAgentIsClaude covers US1/Q4 (FR-019): add with no
// --agent targets Claude Code.
func TestMultiAgentShare_DefaultAgentIsClaude(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := t.TempDir() // no .claude marker, so detection cannot pick an agent

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("default add did not install into Claude Code: %v", err)
	}
}

// TestMultiAgentShare_CollisionDifferentSource covers T020/FR-029: the same
// skill name from a different source is refused with exit 3.
func TestMultiAgentShare_CollisionDifferentSource(t *testing.T) {
	t.Parallel()

	repoA := gitRepo(t, validSkill("demo"), "v1.0.0")
	repoB := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repoA, "--agent", "claude"); code != 0 {
		t.Fatalf("add A: %s", stderr)
	}
	_, stderr, code := runGskill(t, proj, "add", repoB, "--agent", "codex")
	if code != 3 {
		t.Errorf("collision exit = %d, want 3\n%s", code, stderr)
	}
}

// TestMultiAgentShare_ModeFlagsMutuallyExclusive covers T018: --copy and
// --symlink together is a usage error (exit 2).
func TestMultiAgentShare_ModeFlagsMutuallyExclusive(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	_, _, code := runGskill(t, proj, "add", repo, "--copy", "--symlink")
	if code != 2 {
		t.Errorf("--copy --symlink exit = %d, want 2", code)
	}
}

// TestMultiAgentShare_CopyModeRecorded covers US1 (FR-022/FR-024): --copy
// records the copy mode and materializes real content.
func TestMultiAgentShare_CopyModeRecorded(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude", "--copy"); code != 0 {
		t.Fatalf("add --copy: %s", stderr)
	}
	// The agent target is a real directory (a copy), not a symlink.
	target := filepath.Join(proj, ".claude", "skills", "demo")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat target: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("--copy produced a symlink, want a copy")
	}
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"copy"`) {
		t.Errorf("lockfile does not record copy mode:\n%s", lock)
	}
}
