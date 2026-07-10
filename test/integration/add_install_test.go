package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lockEntryGskill decodes one skill's namespaced gskill block from the shared
// lock as a generic map, for asserting on recorded metadata.
func lockEntryGskill(t *testing.T, proj, skill string) map[string]any {
	t.Helper()
	var doc struct {
		Skills map[string]map[string]json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(readFile(t, filepath.Join(proj, "skills-lock.json")), &doc); err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	raw, ok := doc.Skills[skill]["gskill"]
	if !ok {
		t.Fatalf("skill %q has no gskill block", skill)
	}
	var ext map[string]any
	if err := json.Unmarshal(raw, &ext); err != nil {
		t.Fatalf("parse gskill block: %v", err)
	}
	return ext
}

// lockAgents returns a skill's recorded agents from the lock.
func lockAgents(t *testing.T, proj, skill string) []string {
	t.Helper()
	ext := lockEntryGskill(t, proj, skill)
	rawAgents, _ := ext["agents"].([]any)
	agents := make([]string, 0, len(rawAgents))
	for _, a := range rawAgents {
		if s, ok := a.(string); ok {
			agents = append(agents, s)
		}
	}
	return agents
}

// TestAdd_RecordsAgentSet verifies that a bare `add` (no --agent) records the
// resolved default agent in the lock's gskill block, and that adding a second
// agent unions the set (008 FR-001/FR-002).
func TestAdd_RecordsAgentSet(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)

	// Bare add: no --agent, no --version. The default agent (claude) is applied
	// and MUST be recorded in the lock.
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	if got := lockAgents(t, proj, "demo"); len(got) != 1 || got[0] != "claude" {
		t.Errorf("recorded agents = %v, want [claude]", got)
	}

	// Add the same skill for a second agent: the lock unions the set.
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "codex"); code != 0 {
		t.Fatalf("add codex exit %d: %s", code, stderr)
	}
	got := lockAgents(t, proj, "demo")
	if len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Errorf("recorded agents = %v, want [claude codex]", got)
	}
}

// TestAdd_RecordsVersionPin verifies that a bare `add` records the resolved
// tracking intent matching its ref-kind, that explicit input is preserved,
// and that a local (unversioned) source records no pin but keeps its agent
// set (008 FR-004/FR-005/FR-006).
func TestAdd_RecordsVersionPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tags       []string
		local      bool     // add a local non-git dir instead of a git repo
		extraArgs  []string // additional `add` flags
		wantKey    string   // gskill.state key that must be present ("" = none)
		wantVal    string
		wantNoPins bool // assert no requested version/ref/commit at all
	}{
		{name: "semver tag fills caret version", tags: []string{"v1.0.0", "v1.2.0"}, wantKey: "requestedVersion", wantVal: "^1.2.0"},
		{name: "tagless fills mutable ref", tags: nil, wantKey: "requestedRef", wantVal: "HEAD"},
		{name: "explicit version preserved", tags: []string{"v1.0.0"}, extraArgs: []string{"--version", "^1.0.0"}, wantKey: "requestedVersion", wantVal: "^1.0.0"},
		{name: "local source has no pin", local: true, wantNoPins: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proj := newProject(t)
			var src string
			if tt.local {
				src = localSkillDir(t, "demo")
			} else {
				src = gitRepo(t, validSkill("demo"), tt.tags...)
			}
			args := append([]string{"add", src}, tt.extraArgs...)
			if _, stderr, code := runGskill(t, proj, args...); code != 0 {
				t.Fatalf("add exit %d: %s", code, stderr)
			}

			ext := lockEntryGskill(t, proj, "demo")
			state, _ := ext["state"].(map[string]any)
			if tt.wantNoPins {
				for _, k := range []string{"requestedVersion", "requestedRef", "requestedCommit"} {
					if v, ok := state[k]; ok && v != "" {
						t.Errorf("local source unexpectedly recorded %s=%v", k, v)
					}
				}
				if got := lockAgents(t, proj, "demo"); len(got) != 1 || got[0] != "claude" {
					t.Errorf("local source agents = %v, want [claude]", got)
				}
				return
			}
			if got, _ := state[tt.wantKey].(string); got != tt.wantVal {
				t.Errorf("gskill.state.%s = %q, want %q", tt.wantKey, got, tt.wantVal)
			}
		})
	}
}

func TestInitAddInstall_RoundTripAndIdempotent(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init exit %d: %s", code, stderr)
	}

	stdout, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "demo") {
		t.Errorf("add stdout = %q, want skill name", stdout)
	}

	// Skill files appear in the agent skill dir; no manifest is ever written.
	installed := filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("skill not installed at %s: %v", installed, err)
	}
	if _, err := os.Stat(filepath.Join(proj, "gskill.toml")); !os.IsNotExist(err) {
		t.Error("add created a gskill.toml")
	}

	lockBytes, err := os.ReadFile(filepath.Join(proj, "skills-lock.json")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	lockStr := string(lockBytes)
	for _, want := range []string{`"refKind": "semver"`, `"version": "1.2.0"`, `"commit":`, `"storeHash":`, `"claude"`} {
		if !strings.Contains(lockStr, want) {
			t.Errorf("lockfile missing %q:\n%s", want, lockStr)
		}
	}

	// Re-running install reports no changes (idempotent).
	stdout, stderr, code = runGskill(t, proj, "install", "--json")
	if code != 0 {
		t.Fatalf("install exit %d: %s", code, stderr)
	}
	var result struct {
		Changed bool `json:"changed"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("install json: %v\n%s", err, stdout)
	}
	if result.Changed {
		t.Error("install reported changes on idempotent re-run")
	}
}
