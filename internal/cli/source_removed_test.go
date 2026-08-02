package cli

import (
	"strings"
	"testing"
)

// TestSourceCommandRemoved locks the removal of the top-level `source`
// command and all of its former subcommands (spec 021 FR-001/FR-002/FR-008):
// every spelling must fail on the first token as an unknown command with the
// standard usage diagnostic and exit code 2. `search` and `add … --list` —
// the supported discovery and preview paths — keep working.
func TestSourceCommandRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"source"},
		{"source", "--help"},
		{"source", "list", "some-src"},
		{"source", "inspect", "some-src"},
		{"source", "check", "some-src"},
		{"source", "list", "some-src", "--json"},
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

	if _, stderr, code := runCLI(t, nil, "search", "--help"); code != 0 {
		t.Errorf("gskill search --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if _, stderr, code := runCLI(t, nil, "add", "--help"); code != 0 {
		t.Errorf("gskill add --help: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
}
