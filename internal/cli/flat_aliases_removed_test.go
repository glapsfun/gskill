package cli

import (
	"strings"
	"testing"
)

// TestFlatMaintenanceAliasesRemoved locks the removal of the five hidden
// top-level aliases of the `project` maintenance commands (spec 025
// FR-001/FR-002/FR-008): `sync`, `repair`, `verify`, `check`, and `diff` must
// each fail as an unknown command with the standard usage diagnostic and exit
// code 2, in every spelling, while their canonical `project` forms keep
// working.
func TestFlatMaintenanceAliasesRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"sync"},
		{"sync", "--prune"},
		{"sync", "--help"},
		{"repair"},
		{"repair", "--help"},
		{"verify"},
		{"verify", "--help"},
		{"check"},
		{"check", "--fail-on-drift"},
		{"check", "--help"},
		{"diff"},
		{"diff", "--json"},
		{"diff", "--help"},
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

	for _, sub := range []string{"sync", "repair", "verify", "check"} {
		if _, stderr, code := runCLI(t, nil, "project", sub, "--help"); code != 0 {
			t.Errorf("gskill project %s --help: exit code = %d, want 0 (stderr: %q)", sub, code, stderr)
		}
	}
}
