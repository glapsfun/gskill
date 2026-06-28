package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	lockBytes, err := os.ReadFile(filepath.Join(proj, "gskill.lock")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	lockStr := string(lockBytes)
	for _, want := range []string{`"ref_kind": "semver"`, `"version": "1.2.0"`, `"commit":`, `"content_hash":`, `"claude"`} {
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
