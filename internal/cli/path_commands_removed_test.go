package cli

import (
	"strings"
	"testing"
)

// TestPathCommandsRemoved locks the removal of `cache path` and `config path`
// (spec 025 FR-001/FR-002): both were folded into their siblings, so every
// spelling must fail as an unknown command with the standard usage diagnostic
// and exit code 2, while `cache stats` and `config list` — which now carry the
// path — keep working.
func TestPathCommandsRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"cache", "path"},
		{"cache", "path", "--json"},
		{"cache", "path", "--help"},
		{"config", "path"},
		{"config", "path", "--json"},
		{"config", "path", "--help"},
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

	for _, args := range [][]string{{"cache", "stats", "--help"}, {"config", "list", "--help"}} {
		if _, stderr, code := runCLI(t, nil, args...); code != 0 {
			t.Errorf("gskill %s: exit code = %d, want 0 (stderr: %q)", strings.Join(args, " "), code, stderr)
		}
	}
}
