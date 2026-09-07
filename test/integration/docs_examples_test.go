package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file verifies that the copy-paste examples shown in docs/ actually
// produce their documented outcomes (spec FR-010, SC-004). Each subtest names
// the doc page it backs. It reuses the in-process CLI harness (runGskill) so the
// checks are hermetic, offline, and run under the single scripts/verify.sh gate.
//
// TUI, remote-Git, and some error-path examples are verified manually and are
// intentionally not covered here (see contracts/example-verification.md).

// TestDocsExamples_LocalSkillLifecycle backs:
//   - docs/tutorials/getting-started.md
//   - docs/how-to/install-a-local-skill.md
//   - docs/how-to/inspect-list-info-diff.md
//   - docs/how-to/script-with-json.md
func TestDocsExamples_LocalSkillLifecycle(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	skill := localSkillDir(t, "demo")

	// init
	initProject(t, proj)

	// add a local skill (offline, no network)
	if _, stderr, code := runGskill(t, proj, "add", skill); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	// the skill is installed into the detected agent's dir
	installed := filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("skill not installed at %s: %v", installed, err)
	}

	// list --json emits parseable JSON on stdout
	stdout, stderr, code := runGskill(t, proj, "list", "--json")
	if code != 0 {
		t.Fatalf("list --json exit %d: %s", code, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("list --json stdout is not valid JSON:\n%s", stdout)
	}

	// install is idempotent: a re-run reports no changes
	stdout, stderr, code = runGskill(t, proj, "install", "--json")
	if code != 0 {
		t.Fatalf("install exit %d: %s", code, stderr)
	}
	var res struct {
		Changed bool `json:"changed"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("install --json: %v\n%s", err, stdout)
	}
	if res.Changed {
		t.Error("install reported changes on an idempotent re-run")
	}
}

// TestDocsExamples_VerifyDetectsTampering backs docs/how-to/verify-integrity.md:
// a clean verify exits 0; a single tampered byte exits 6.
func TestDocsExamples_VerifyDetectsTampering(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	skill := localSkillDir(t, "demo")

	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", skill); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	if _, stderr, code := runGskill(t, proj, "verify"); code != 0 {
		t.Fatalf("clean verify exit: %s", stderr)
	}

	target := filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")
	if err := os.WriteFile(target, append(readFile(t, target), '!'), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runGskill(t, proj, "verify"); code != 6 {
		t.Errorf("verify after tamper exit = %d, want 6 (integrity failure)", code)
	}
}

// TestDocsExamples_FrozenLockfile backs docs/how-to/reproduce-with-frozen-lockfile.md:
// a matching lock restores cleanly (exit 0) after a simulated clean checkout.
func TestDocsExamples_FrozenLockfile(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)

	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Matching lock restores cleanly after a simulated clean checkout.
	if err := os.RemoveAll(filepath.Join(proj, ".gskill")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile"); code != 0 {
		t.Fatalf("frozen restore exit: %s", stderr)
	}
}

// TestDocsExamples_JSONStatusCommands backs docs/how-to/script-with-json.md and
// docs/how-to/gate-ci-on-drift.md: status commands emit valid JSON for scripting.
func TestDocsExamples_JSONStatusCommands(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	skill := localSkillDir(t, "demo")

	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", skill); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	for _, args := range [][]string{
		{"check"},
		{"outdated"},
		{"verify"},
		{"update", "--list"},
		{"update", "--list", "--all"},
	} {
		full := append([]string{"--json"}, args...)
		stdout, stderr, code := runGskill(t, proj, full...)
		if code != 0 {
			t.Fatalf("%v exit %d: %s", full, code, stderr)
		}
		if !json.Valid([]byte(stdout)) {
			t.Errorf("%v stdout is not valid JSON:\n%s", full, stdout)
		}
	}
}

// TestDocsExamples_CustomizeASkill backs:
//   - docs/how-to/customize-a-skill.md
//   - docs/reference/manifest.md
//
// The page tells a reader to write a file, declare an append override, and run
// install. If that stops producing the documented outcome, the page is wrong
// and this test is how we find out.
func TestDocsExamples_CustomizeASkill(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("code-review"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// As documented: write the content, declare it, install.
	rules := filepath.Join(proj, "gskill", "code-review", "house-rules.md")
	if err := os.MkdirAll(filepath.Dir(rules), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules, []byte("\n## House rules\nAlways cite file:line.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declareOverride(t, proj, "code-review", "append = [\"gskill/code-review/house-rules.md\"]\n")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}

	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "code-review", "SKILL.md")) //nolint:gosec // test project
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "House rules") {
		t.Errorf("documented append did not reach the committed content:\n%s", content)
	}

	assertDocumentedSurface(t, proj)

	// And that editing the input afterwards is reported, as the page says.
	if err := os.WriteFile(rules, []byte("\n## House rules\nAlso: never guess.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// check reports drift in its output; a non-zero exit is reserved for
	// --fail-on-drift, which is the contract the page describes.
	checkOut, checkErr, _ := runGskill(t, proj, "check")
	if !strings.Contains(checkOut+checkErr, "house-rules.md") {
		t.Errorf("check does not name the changed input, contrary to the page:\n%s\n%s", checkOut, checkErr)
	}
	if _, _, code := runGskill(t, proj, "check", "--fail-on-drift"); code != 7 {
		t.Errorf("check --fail-on-drift = exit %d, want 7", code)
	}
}

// assertDocumentedSurface checks the surface docs/how-to/customize-a-skill.md
// promises: list marks the skill, and info reports the identity keys.
func assertDocumentedSurface(t *testing.T, proj string) {
	t.Helper()

	stdout, _, _ := runGskill(t, proj, "list")
	if !strings.Contains(stdout, "(overridden)") {
		t.Errorf("list does not mark the skill as documented:\n%s", stdout)
	}
	infoOut, _, _ := runGskill(t, proj, "info", "code-review", "--json")
	var info map[string]any
	if err := json.Unmarshal([]byte(infoOut), &info); err != nil {
		t.Fatalf("info --json invalid: %v", err)
	}
	for _, key := range []string{"overridden", "base_hash", "override_digest"} {
		if _, ok := info[key]; !ok {
			t.Errorf("info --json omits documented key %q", key)
		}
	}
}

// TestDocsExamples_UpgradeASkill backs docs/how-to/upgrade-a-skill.md: the
// documented --latest, positional, --to, --dry-run, and refusal flows.
func TestDocsExamples_UpgradeASkill(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v2.0.0")
	publishNewVersion(t, repo, "demo", "v2.1.0")

	if stdout, stderr, code := runGskill(t, proj, "--dry-run", "upgrade", "demo"); code != 0 || !strings.Contains(stdout, "Dry run") {
		t.Fatalf("dry run: %d %s\n%s", code, stderr, stdout)
	}
	if stdout, stderr, code := runGskill(t, proj, "upgrade", "demo", "--latest"); code != 0 || !strings.Contains(stdout, "verified") {
		t.Fatalf("upgrade --latest: %d %s\n%s", code, stderr, stdout)
	}
	if !strings.Contains(string(readFile(t, manifestPath(proj))), `version = "^2.1.0"`) {
		t.Errorf("manifest not moved:\n%s", readFile(t, manifestPath(proj)))
	}
	// A caret range keeps its shape: the target becomes the range's floor,
	// and the lock resolves to the newest release that range still allows.
	if stdout, stderr, code := runGskill(t, proj, "upgrade", "demo", "2.0.0"); code != 0 || !strings.Contains(stdout, "redeclared") {
		t.Fatalf("positional target: %d %s\n%s", code, stderr, stdout)
	}
	if !strings.Contains(string(readFile(t, manifestPath(proj))), `version = "^2.0.0"`) {
		t.Errorf("positional target not written:\n%s", readFile(t, manifestPath(proj)))
	}
	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "--to", "9.9.9"); code != 2 {
		t.Errorf("unknown target exit = %d, want 2: %s", code, stderr)
	}
	if stdout, _, code := runGskill(t, proj, "--json", "upgrade", "demo", "--to", "2.1.0"); code != 0 {
		t.Errorf("json upgrade exit = %d", code)
	} else {
		assertSingleJSON(t, stdout, "upgrade --json")
	}
}
