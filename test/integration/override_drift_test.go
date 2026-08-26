package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installWithOverride sets up a project with an appended override and returns
// the path of the referenced input file.
func installWithOverride(t *testing.T, proj string) string {
	t.Helper()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
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
	return rules
}

// TestOverrideDrift_ReportedByCheck is FR-010 and SC-003: editing a referenced
// override input must surface. Without this the committed content silently
// stops matching the declaration that produced it, and "reproducible" becomes
// a claim gskill cannot back.
func TestOverrideDrift_ReportedByCheck(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	rules := installWithOverride(t, proj)

	if _, _, code := runGskill(t, proj, "check"); code != 0 {
		t.Fatalf("check reported drift before anything was edited (exit %d)", code)
	}

	if err := os.WriteFile(rules, []byte("## House rules\nAlso: never guess.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, _ := runGskill(t, proj, "check")
	combined := stdout + stderr
	if !strings.Contains(combined, "demo") {
		t.Errorf("check did not name the drifted skill:\n%s", combined)
	}
	if !strings.Contains(combined, "rules.md") && !strings.Contains(strings.ToLower(combined), "override") {
		t.Errorf("check did not identify the override input as the cause:\n%s", combined)
	}
}

// TestOverrideDrift_DistinctFromContentDrift is contract assertion C4: a script
// must be able to tell "my override input changed" from "someone edited the
// installed skill", because the remedies differ.
func TestOverrideDrift_DistinctFromContentDrift(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	rules := installWithOverride(t, proj)
	if err := os.WriteFile(rules, []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, _ := runGskill(t, proj, "check", "--json")
	if !strings.Contains(stdout, "override") {
		t.Errorf("--json output does not distinguish override drift:\n%s", stdout)
	}
	var doc any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Errorf("check --json is not valid JSON: %v\n%s", err, stdout)
	}
}

// TestOverrideDrift_FrozenFailsClosed is US3 scenario 2: a frozen run must
// refuse rather than quietly re-materialize to match the edited inputs.
func TestOverrideDrift_FrozenFailsClosed(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	rules := installWithOverride(t, proj)
	lockBefore := readLock(t, proj)

	if err := os.WriteFile(rules, []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile")
	if code == 0 {
		t.Fatal("frozen install accepted an edited override input")
	}
	if code != 4 && code != 6 {
		t.Errorf("exit = %d, want 4 (lock mismatch) or 6 (integrity): %s", code, stderr)
	}
	if after := readLock(t, proj); after != lockBefore {
		t.Error("frozen run rewrote the lock")
	}
}

// TestOverrideDrift_InstallReMaterializes: a plain install accepts the edit and
// brings the committed content back in line with the declaration.
func TestOverrideDrift_InstallReMaterializes(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	rules := installWithOverride(t, proj)
	if err := os.WriteFile(rules, []byte("## House rules\nAlso: never guess.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: exit %d: %s", code, stderr)
	}
	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")) //nolint:gosec // test project
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "never guess") {
		t.Errorf("install did not re-materialize against the edited input:\n%s", content)
	}
	if _, _, code := runGskill(t, proj, "check"); code != 0 {
		t.Errorf("check still reports drift after a re-install (exit %d)", code)
	}
}
