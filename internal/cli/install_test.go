package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// lockOnlyProject builds a directory containing only a skills-lock.json whose
// two entries point at a local source tree (offline-friendly).
func lockOnlyProject(t *testing.T) (dir string) {
	t.Helper()
	src := addSourceTree(t, "alpha", "beta")
	dir = t.TempDir()
	entry := func(name string) string {
		hash, err := integrity.CompatHash(filepath.Join(src, "skills", name))
		if err != nil {
			t.Fatal(err)
		}
		return `    "` + name + `": {
      "source": "` + strings.ReplaceAll(src, `\`, `\\`) + `",
      "sourceType": "local",
      "skillPath": "skills/` + name + `/SKILL.md",
      "computedHash": "` + hash + `"
    }`
	}
	lock := "{\n  \"version\": 1,\n  \"skills\": {\n" + entry("alpha") + ",\n" + entry("beta") + "\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func agentDirsExist(t *testing.T, dir string, agents ...string) {
	t.Helper()
	for _, ag := range agents {
		for _, skill := range []string{"alpha", "beta"} {
			if _, err := os.Stat(filepath.Join(dir, "."+ag, "skills", skill)); err != nil {
				t.Errorf("target %s/%s missing: %v", ag, skill, err)
			}
		}
	}
}

// TestInstall_AgentFlagForms (T024/FR-012): comma-separated and repeated
// --agent produce identical results.
func TestInstall_AgentFlagForms(t *testing.T) {
	t.Parallel()

	comma := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", comma, "install", "--agent", "claude,codex"); code != 0 {
		t.Fatalf("comma form: code %d, stderr %q", code, stderr)
	}
	agentDirsExist(t, comma, "claude", "codex")

	repeated := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", repeated, "install", "--agent", "claude", "--agent", "codex"); code != 0 {
		t.Fatalf("repeated form: code %d, stderr %q", code, stderr)
	}
	agentDirsExist(t, repeated, "claude", "codex")
}

// TestInstall_FlagConflicts (T024): incompatible or invalid flags exit 2.
func TestInstall_FlagConflicts(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)

	if _, _, code := runCLI(t, newTestApp(), "-C", dir, "install", "--force", "--frozen-lockfile"); code != 2 {
		t.Errorf("--force --frozen-lockfile: code = %d, want 2", code)
	}
	if _, _, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--install-mode", "bogus"); code != 2 {
		t.Errorf("--install-mode bogus: code = %d, want 2", code)
	}
}

// TestInstall_CopyAliasRecordsMode (T024): --copy is a deprecated alias for
// --install-mode copy and lands in the recorded metadata.
func TestInstall_CopyAliasRecordsMode(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--copy"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	l, err := skillslock.Load(filepath.Join(dir, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := l.Entry("alpha")
	if !ok || e.Ext == nil {
		t.Fatalf("alpha entry/ext missing")
	}
	if e.Ext.InstallMode != "copy" {
		t.Errorf("installMode = %q, want copy", e.Ext.InstallMode)
	}
}

// TestInstall_NoInitRefuses (T024/FR-019): --no-init on an uninitialized
// project fails instead of scaffolding.
func TestInstall_NoInitRefuses(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--no-init")
	if code == 0 {
		t.Fatal("want failure with --no-init on an uninitialized project")
	}
	if !strings.Contains(stderr, "no-init") && !strings.Contains(stderr, "not initialized") {
		t.Errorf("stderr %q should explain the refusal", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill.toml")); err == nil {
		t.Error("gskill.toml was created despite --no-init")
	}
}

// TestInstall_DryRunWritesNothing (T026/FR-015): the plan is reported and the
// tree is untouched.
func TestInstall_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	before, err := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--dry-run")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "alpha") {
		t.Errorf("dry-run output should list the plan:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill.toml")); err == nil {
		t.Error("dry-run created gskill.toml")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); err == nil {
		t.Error("dry-run activated agent targets")
	}
	after, _ := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if string(before) != string(after) {
		t.Error("dry-run modified the lock")
	}
}

// TestInstall_JSONShape (T026): --json emits the documented result document.
func TestInstall_JSONShape(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var doc struct {
		Changed     bool     `json:"changed"`
		Initialized bool     `json:"initialized"`
		Migrated    bool     `json:"migrated"`
		Agents      []string `json:"agents"`
		Skills      []struct {
			Name         string `json:"name"`
			Status       string `json:"status"`
			ComputedHash string `json:"computedHash"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, stdout)
	}
	if !doc.Changed || !doc.Initialized {
		t.Errorf("doc = %+v, want changed+initialized", doc)
	}
	if len(doc.Agents) != 1 || doc.Agents[0] != "claude" {
		t.Errorf("agents = %v", doc.Agents)
	}
	if len(doc.Skills) != 2 || doc.Skills[0].Status != "installed" || len(doc.Skills[0].ComputedHash) != 64 {
		t.Errorf("skills = %+v", doc.Skills)
	}
}
