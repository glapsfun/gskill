package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRun_ErrorWithHintRendersArrowLine(t *testing.T) {
	t.Parallel()

	// `install` in an empty directory fails with the missing-lock error, which
	// must carry the add hint (FR-010).
	dir := t.TempDir()
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install")
	if code == 0 {
		t.Fatal("install in an empty dir succeeded, want a missing-lock error")
	}
	if !strings.Contains(stderr, "error:") {
		t.Errorf("stderr = %q, want an error line", stderr)
	}
	if !strings.Contains(stderr, "→ run 'gskill add <source>'") {
		t.Errorf("stderr = %q, want the add hint arrow line", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on error", stdout)
	}
}

func TestRun_JSONStdoutStaysCleanOnHintedError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "--json", "install")
	if code == 0 {
		t.Fatal("install in an empty dir succeeded, want a missing-manifest error")
	}
	if strings.Contains(stdout, "→") || strings.Contains(stdout, "hint") {
		t.Errorf("stdout = %q, must not carry human-oriented hint text in --json mode", stdout)
	}
	if stdout != "" {
		// Whatever lands on stdout in JSON mode must be valid JSON.
		var v any
		if err := json.Unmarshal([]byte(stdout), &v); err != nil {
			t.Errorf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
		}
	}
	if !strings.Contains(stderr, "→ ") {
		t.Errorf("stderr = %q, want the hint arrow line even in --json mode", stderr)
	}
}

// TestHintAudit_RepresentativeSites exercises the audited user-facing error
// sites and asserts each ends with its actionable hint (FR-010, SC-005).
//
// Reviewed no-next-step exception list — error categories that intentionally
// carry no hint, completing SC-005's 100% accounting:
//   - source unavailable / network (exit 5): the cause is outside gskill
//     (connectivity, remote outage); the error already names the source.
//   - authentication failure (exit 11): remediation depends on the remote's
//     credential setup, which gskill cannot know.
//   - cache/lock failure (exit 12): transient contention; the error text
//     already says another gskill process holds the lock.
//   - generic internal errors (exit 1): unexpected by definition; no single
//     next step exists.
//   - `source check` problems (exit 3): the command's own stdout lists every
//     invalid/duplicate skill with per-skill diagnostics — the output IS the
//     next step.
func TestHintAudit_RepresentativeSites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		init     bool // run inside an inited project
		args     []string
		wantHint string
	}{
		{"install without lock", false, []string{"install"}, "run 'gskill add <source>' to install a first skill"},
		{"unlink unknown agent", true, []string{"unlink", "foo", "--agent", "no-such-agent"}, "run 'gskill doctor' to list detected agents"},
		{"unlink undeclared skill", true, []string{"unlink", "foo", "--agent", "claude"}, "run 'gskill list' to see installed skills"},
		{"config get unknown key", false, []string{"config", "get", "no_such_key"}, "run 'gskill config list' to see available keys"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.init {
				dir = initedProject(t)
			}
			_, stderr, code := runCLI(t, newTestApp(), append([]string{"-C", dir}, tt.args...)...)
			if code == 0 {
				t.Fatalf("gskill %v succeeded, want an error", tt.args)
			}
			if !strings.Contains(stderr, "→ "+tt.wantHint) {
				t.Errorf("gskill %v stderr = %q, want hint %q", tt.args, stderr, tt.wantHint)
			}
		})
	}
}

func TestRun_QuietSuppressesHint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "--quiet", "install")
	if code == 0 {
		t.Fatal("install in an empty dir succeeded, want a missing-manifest error")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty under --quiet", stderr)
	}
}
