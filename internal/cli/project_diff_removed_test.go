package cli

import (
	"regexp"
	"strings"
	"testing"
)

// TestProjectDiffRemoved locks the removal of the `project diff` subcommand
// (spec 025 FR-001/FR-002/FR-006): its report was a strict subset of
// `project check`, so every spelling must now fail as an unknown command with
// exit code 2 and an empty stdout.
func TestProjectDiffRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"project", "diff"},
		{"project", "diff", "--json"},
		{"project", "diff", "--help"},
	} {
		stdout, stderr, code := runCLI(t, nil, args...)
		if code != 2 {
			t.Errorf("gskill %s: exit code = %d, want 2 (usage error)", strings.Join(args, " "), code)
		}
		if stderr == "" {
			t.Errorf("gskill %s: stderr empty, want an unknown-command diagnostic", strings.Join(args, " "))
		}
		if stdout != "" {
			t.Errorf("gskill %s: stdout = %q, want empty for an unknown command", strings.Join(args, " "), stdout)
		}
	}
}

// TestProjectGroupListsSurvivingSubcommands is the other half of the contract:
// the group itself keeps working and advertises exactly the four surviving
// maintenance commands.
func TestProjectGroupListsSurvivingSubcommands(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI(t, nil, "project")
	if code != 0 {
		t.Fatalf("gskill project: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	for _, sub := range []string{"sync", "repair", "verify", "check"} {
		re := regexp.MustCompile(`(?m)^\s+project ` + sub + `\b`)
		if !re.MatchString(stdout) {
			t.Errorf("project group help missing subcommand %q:\n%s", sub, stdout)
		}
	}
	if regexp.MustCompile(`(?m)^\s+project diff\b`).MatchString(stdout) {
		t.Errorf("project group help still lists the removed `diff` subcommand:\n%s", stdout)
	}
}
