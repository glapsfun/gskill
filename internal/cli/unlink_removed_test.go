package cli

import (
	"strings"
	"testing"
)

// TestUnlinkCommandRemoved locks the removal of the top-level `unlink`
// command (spec 021 FR-001/FR-002/FR-007): every spelling must fail as an
// unknown command with the standard usage diagnostic and exit code 2, while
// the supported replacements — exact-set `install --agent` and `remove` —
// and the distinct `store status` subcommand keep working.
func TestUnlinkCommandRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"unlink"},
		{"unlink", "demo"},
		{"unlink", "demo", "--agent", "claude"},
		{"unlink", "demo", "--prune"},
		{"unlink", "--help"},
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

	if _, stderr, code := runCLI(t, nil, "install", "--help"); code != 0 {
		t.Errorf("gskill install --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if _, stderr, code := runCLI(t, nil, "remove", "--help"); code != 0 {
		t.Errorf("gskill remove --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if _, stderr, code := runCLI(t, nil, "store", "status", "--help"); code != 0 {
		t.Errorf("gskill store status --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
}
