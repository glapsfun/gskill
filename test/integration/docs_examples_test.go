package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init exit %d: %s", code, stderr)
	}

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

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
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

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
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

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", skill); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	for _, cmd := range []string{"check", "outdated", "verify"} {
		stdout, stderr, code := runGskill(t, proj, cmd, "--json")
		if code != 0 {
			t.Fatalf("%s --json exit %d: %s", cmd, code, stderr)
		}
		if !json.Valid([]byte(stdout)) {
			t.Errorf("%s --json stdout is not valid JSON:\n%s", cmd, stdout)
		}
	}
}
