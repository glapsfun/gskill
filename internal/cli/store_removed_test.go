package cli

import (
	"strings"
	"testing"
)

// TestStoreCommandRemoved locks the removal of the `store` command tree
// (spec 022 FR-011): the global content store is retired — committed repo
// content replaced it. Every spelling must fail as an unknown command with
// the standard usage diagnostic and exit code 2.
func TestStoreCommandRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"store"},
		{"store", "status"},
		{"store", "list"},
		{"store", "inspect", "sha256:abc"},
		{"store", "verify"},
		{"store", "repair"},
		{"store", "gc"},
		{"store", "pin", "sha256:abc"},
		{"store", "unpin", "sha256:abc"},
		{"store", "pins"},
		{"store", "--help"},
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

// TestProjectsCommandRemoved locks the removal of the `projects` command tree
// (spec 022 FR-011): the advisory project registry is retired.
func TestProjectsCommandRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"projects"},
		{"projects", "list"},
		{"projects", "inspect", "p-abc"},
		{"projects", "prune"},
		{"projects", "refresh"},
		{"projects", "--help"},
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

// TestMigrateCommandRemoved locks the removal of `migrate global-store`
// (spec 022 FR-013): migration is transparent on mutating commands; there is
// no standalone migrate command.
func TestMigrateCommandRemoved(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"migrate"},
		{"migrate", "global-store"},
		{"migrate", "--help"},
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
