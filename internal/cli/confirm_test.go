package cli

import (
	"bytes"
	"strings"
	"testing"
)

// newInteractiveOutput builds an Output with interactivity forced on and a
// scripted stdin, bypassing the TTY detection in NewOutput.
func newInteractiveOutput(stdout, stderr *bytes.Buffer, stdin string) *Output {
	return &Output{
		stdout:      stdout,
		stderr:      stderr,
		interactive: true,
		stdin:       strings.NewReader(stdin),
	}
}

func TestConfirm_AssumeYesSkipsPrompt(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := newInteractiveOutput(&out, &errb, "n\n")
	if !o.Confirm("Remove foo?", true) {
		t.Error("Confirm with assumeYes = false, want true")
	}
	if errb.Len() != 0 {
		t.Errorf("stderr = %q, want no prompt under --yes", errb.String())
	}
}

func TestConfirm_NonInteractiveProceedsWithoutPrompt(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := NewOutput(&out, &errb, OutputOptions{Interactive: true}) // buffer != TTY → non-interactive
	if !o.Confirm("Remove foo?", false) {
		t.Error("Confirm in a non-interactive session = false, want true (never block CI)")
	}
	if errb.Len() != 0 {
		t.Errorf("stderr = %q, want no prompt when non-interactive", errb.String())
	}
}

func TestConfirm_InteractiveReadsAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reply string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false}, // default is No
	}
	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.reply)+"_reply", func(t *testing.T) {
			t.Parallel()

			var out, errb bytes.Buffer
			o := newInteractiveOutput(&out, &errb, tt.reply)
			if got := o.Confirm("Remove foo?", false); got != tt.want {
				t.Errorf("Confirm(reply %q) = %v, want %v", tt.reply, got, tt.want)
			}
			if !strings.Contains(errb.String(), "Remove foo? [y/N]") {
				t.Errorf("stderr = %q, want the [y/N] prompt", errb.String())
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, prompts must stay on stderr", out.String())
			}
		})
	}
}

func TestDestructiveOps_ProceedNonInteractively(t *testing.T) {
	t.Parallel()

	// In tests stdout is a buffer (never a TTY), so these runs are
	// non-interactive: remove and sync --prune must proceed without any
	// prompt or blocking read (FR-012). Sync needs a lock file to operate on
	// (a missing lock fails closed), so init with an empty one.
	dir := t.TempDir()
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "init", "--lock"); code != 0 {
		t.Fatalf("init --lock: %s", stderr)
	}
	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "project", "sync", "--prune")
	if code != 0 {
		t.Fatalf("project sync --prune: exit code = %d, stderr: %q", code, stderr)
	}
	if strings.Contains(stderr, "[y/N]") {
		t.Errorf("stderr = %q, want no confirmation prompt non-interactively", stderr)
	}

	_, stderr, _ = runCLI(t, newTestApp(), "-C", dir, "--yes", "remove", "nonexistent-skill")
	if strings.Contains(stderr, "[y/N]") {
		t.Errorf("stderr = %q, want no confirmation prompt under --yes", stderr)
	}
}
