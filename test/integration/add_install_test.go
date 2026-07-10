package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// section returns the lines of the named TOML table (e.g. "[skills.demo]") up to
// the next table header, for asserting on a single skill entry.
func section(toml, header string) string {
	idx := strings.Index(toml, header)
	if idx < 0 {
		return ""
	}
	rest := toml[idx+len(header):]
	if end := strings.Index(rest, "\n["); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestAdd_RecordsAgentSet verifies that a bare `add` (no --agent) records the
// resolved default agent in the manifest, and that adding a second agent unions
// the set (008 FR-001/FR-002).
func TestAdd_RecordsAgentSet(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init exit %d: %s", code, stderr)
	}

	// Bare add: no --agent, no --version. The default agent (claude) is applied
	// and MUST be recorded in the manifest.
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	manifestBytes := readFile(t, filepath.Join(proj, "gskill.toml"))
	demo := section(string(manifestBytes), "[skills.demo]")
	if !strings.Contains(demo, "agents = ['claude']") {
		t.Errorf("manifest demo entry missing agents = ['claude']:\n%s", demo)
	}

	// Add the same skill for a second agent: the manifest unions the set.
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "codex"); code != 0 {
		t.Fatalf("add codex exit %d: %s", code, stderr)
	}
	manifestBytes = readFile(t, filepath.Join(proj, "gskill.toml"))
	demo = section(string(manifestBytes), "[skills.demo]")
	if !strings.Contains(demo, "agents = ['claude', 'codex']") {
		t.Errorf("manifest demo entry missing unioned agents:\n%s", demo)
	}
}

// TestSync_RespectsDefaultsAgentsBlock verifies that when a [defaults] agents
// block is present and a skill inherits from it, sync does NOT freeze the
// resolved agent set into the per-skill entry (it keeps inheriting), while still
// backfilling the version pin (008 FR-003).
func TestSync_RespectsDefaultsAgentsBlock(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	manifest := "schema_version = 1\n\n[defaults]\nagents = ['claude', 'codex']\n\n" +
		"[skills.demo]\nsource = '" + repo + "'\npath = 'demo'\n"
	if err := os.WriteFile(filepath.Join(proj, "gskill.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("sync exit %d: %s", code, stderr)
	}

	got := string(readFile(t, filepath.Join(proj, "gskill.toml")))
	// "agents" appears only once — in [defaults], not under [skills.demo].
	if n := strings.Count(got, "agents"); n != 1 {
		t.Errorf("expected agents only in [defaults] (count 1), got %d:\n%s", n, got)
	}
	// Both agents still materialized from the defaults block.
	for _, agentDir := range []string{".claude", ".codex"} {
		if _, err := os.Stat(filepath.Join(proj, agentDir, "skills", "demo")); err != nil {
			t.Errorf("skill not installed for %s: %v", agentDir, err)
		}
	}
}

// TestAdd_RecordsVersionPin verifies that a bare `add` records the resolved
// version in the manifest field matching its ref-kind, that explicit input is
// preserved, and that a local (unversioned) source records no pin but keeps its
// agent set (008 FR-004/FR-005/FR-006).
func TestAdd_RecordsVersionPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tags       []string
		local      bool     // add a local non-git dir instead of a git repo
		extraArgs  []string // additional `add` flags
		wantPin    string   // a manifest line that must be present ("" = none)
		wantNoPins bool     // assert no version/ref/commit at all
	}{
		{name: "semver tag fills caret version", tags: []string{"v1.0.0", "v1.2.0"}, wantPin: "version = '^1.2.0'"},
		{name: "tagless fills mutable ref", tags: nil, wantPin: "ref = 'HEAD'"},
		{name: "explicit version preserved", tags: []string{"v1.0.0"}, extraArgs: []string{"--version", "^1.0.0"}, wantPin: "version = '^1.0.0'"},
		{name: "local source has no pin", local: true, wantNoPins: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proj := newProject(t)
			if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
				t.Fatalf("init exit %d: %s", code, stderr)
			}

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

			demo := section(string(readFile(t, filepath.Join(proj, "gskill.toml"))), "[skills.demo]")
			if tt.wantNoPins {
				for _, k := range []string{"version =", "ref =", "commit ="} {
					if strings.Contains(demo, k) {
						t.Errorf("local source unexpectedly recorded %q:\n%s", k, demo)
					}
				}
				if !strings.Contains(demo, "agents = ['claude']") {
					t.Errorf("local source missing agent set:\n%s", demo)
				}
				return
			}
			if !strings.Contains(demo, tt.wantPin) {
				t.Errorf("manifest demo entry missing %q:\n%s", tt.wantPin, demo)
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

	// Skill files appear in the agent skill dir.
	installed := filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("skill not installed at %s: %v", installed, err)
	}

	// Manifest records intent; lockfile records resolved reality.
	manifestBytes, err := os.ReadFile(filepath.Join(proj, "gskill.toml")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifestBytes), "[skills.demo]") {
		t.Errorf("manifest missing skill entry:\n%s", manifestBytes)
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
