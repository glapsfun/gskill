package cli

import (
	"strings"
	"testing"
)

// TestStatusCommandRemoved locks the removal of the top-level `status` alias
// (spec 020 FR-002/FR-003): `gskill status` must fail as an unknown command
// with the standard usage diagnostic and exit code 2, in every spelling,
// while `list` — its former canonical form — and the distinct `store status`
// subcommand (FR-004) keep working.
func TestStatusCommandRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"status"},
		{"status", "--json"},
		{"status", "--help"},
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

	if _, stderr, code := runCLI(t, nil, "list", "--help"); code != 0 {
		t.Errorf("gskill list --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if _, stderr, code := runCLI(t, nil, "store", "status", "--help"); code != 0 {
		t.Errorf("gskill store status --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
}
