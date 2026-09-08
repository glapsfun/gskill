package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck_FailOnDriftGating(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Clean install: --fail-on-drift exits 0.
	if _, stderr, code := runGskill(t, proj, "project", "check", "--fail-on-drift"); code != 0 {
		t.Fatalf("clean check --fail-on-drift exit %d: %s", code, stderr)
	}

	// Induce drift by removing the installed skill.
	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills", "demo")); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runGskill(t, proj, "project", "check", "--fail-on-drift"); code != 7 {
		t.Errorf("drift exit code = %d, want 7", code)
	}
	// Without the flag, check reports drift but exits 0.
	if _, _, code := runGskill(t, proj, "project", "check"); code != 0 {
		t.Errorf("check without --fail-on-drift exit = %d, want 0", code)
	}
}

// TestCheck_ReportsEveryFieldProjectDiffDid is spec 025 FR-006: `project diff`
// was retired because its report — one name and one status per skill — is
// already available elsewhere. That claim only holds if it is asserted, not
// assumed, so this test pins both halves of the replacement:
//
//   - the scripted half is `project check --json`, whose skills array carries
//     a name and a status for every locked skill, drift or no drift;
//   - the human half is `gskill list`, which prints the per-skill name/status
//     table `project diff` printed.
//
// It is deliberately NOT `project check`'s human output: that is a summary
// line by design, and FR-005 requires every surviving `project` command to
// behave byte-identically, which outranks reshaping one of them here.
func TestCheck_ReportsEveryFieldProjectDiffDid(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	assertNamedStatuses := func(t *testing.T, label string) {
		t.Helper()

		stdout, stderr, code := runGskill(t, proj, "--json", "project", "check")
		if code != 0 && code != 7 {
			t.Fatalf("%s: project check --json exit %d: %s", label, code, stderr)
		}
		skills := parseCheckSkills(t, label, stdout)
		if len(skills) == 0 {
			t.Fatalf("%s: project check --json reported no skills:\n%s", label, stdout)
		}
		assertCheckPayload(t, label, skills, stdout)

		// The human-readable half of the replacement: `list` still prints the
		// per-skill name/status table that `project diff` printed.
		human, listErr, listCode := runGskill(t, proj, "--no-interactive", "list")
		if listCode != 0 {
			t.Fatalf("%s: list exit %d: %s", label, listCode, listErr)
		}
		assertListRows(t, label, skills, human)
	}

	// Sequential by necessity, not subtests: the second assertion runs against
	// the drift the first one must not see.
	assertNamedStatuses(t, "clean")

	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	assertNamedStatuses(t, "drifted")
}

// rowFor returns the line of a `gskill list` table that describes skill name.
func rowFor(table, name string) (string, bool) {
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), name) {
			return line, true
		}
	}
	return "", false
}

// checkSkill is one entry of `project check --json`'s skills array — exactly
// the two fields the retired `project diff` reported.
type checkSkill struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// parseCheckSkills decodes the skills array from a `project check --json` run.
func parseCheckSkills(t *testing.T, label, stdout string) []checkSkill {
	t.Helper()

	var report struct {
		HasDrift bool         `json:"has_drift"`
		Skills   []checkSkill `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("%s: project check --json is not valid JSON: %v\n%s", label, err, stdout)
	}
	return report.Skills
}

// assertCheckPayload pins the scripted half of the replacement: every entry
// carries both fields, for every skill, drift or no drift.
func assertCheckPayload(t *testing.T, label string, skills []checkSkill, stdout string) {
	t.Helper()

	for _, s := range skills {
		if s.Name == "" {
			t.Errorf("%s: a skill entry has no name:\n%s", label, stdout)
		}
		if s.Status == "" {
			t.Errorf("%s: skill %q has no status:\n%s", label, s.Name, stdout)
		}
	}
}

// assertListRows pins the human half: `list` names every skill and carries its
// status on the same row. Name alone is not the claim — `project diff` printed
// name AND status, and this test is the evidence for retiring it.
func assertListRows(t *testing.T, label string, skills []checkSkill, table string) {
	t.Helper()

	for _, s := range skills {
		row, ok := rowFor(table, s.Name)
		if !ok {
			t.Errorf("%s: list does not name skill %q:\n%s", label, s.Name, table)
			continue
		}
		if !strings.Contains(row, s.Status) {
			t.Errorf("%s: list row for %q does not carry status %q:\n\t%s",
				label, s.Name, s.Status, row)
		}
	}
}
