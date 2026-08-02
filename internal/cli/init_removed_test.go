package cli

import (
	"strings"
	"testing"
)

// TestInitCommandRemoved locks the removal of the top-level `init` command
// (spec 021 FR-001/FR-002): every spelling must fail as an unknown command
// with the standard usage diagnostic and exit code 2, while `add` and
// `install` — the auto-initializing lifecycle commands that replace it —
// keep working.
func TestInitCommandRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"init"},
		{"init", "--lock"},
		{"init", "--help"},
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

	if _, stderr, code := runCLI(t, nil, "add", "--help"); code != 0 {
		t.Errorf("gskill add --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if _, stderr, code := runCLI(t, nil, "install", "--help"); code != 0 {
		t.Errorf("gskill install --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
}
