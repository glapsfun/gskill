package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
)

// TestRemoveRequiresForce (spec 016, contracts/cli-remove-force.md §2): the
// remove confirmation gate must abort only when a session is non-interactive
// and no opt-in (--yes or --force) was supplied; every other combination
// defers to the existing Confirm prompt or an already-granted opt-in. Callers
// compute optedIn as g.Yes || c.Force before calling this predicate.
func TestRemoveRequiresForce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interactive bool
		optedIn     bool
		want        bool
	}{
		{"non-interactive, no opt-in", false, false, true},
		{"non-interactive, opted in", false, true, false},
		{"interactive, no opt-in", true, false, false},
		{"interactive, opted in", true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := removeRequiresForce(tt.interactive, tt.optedIn); got != tt.want {
				t.Errorf("removeRequiresForce(%v, %v) = %v, want %v", tt.interactive, tt.optedIn, got, tt.want)
			}
		})
	}
}

// installedAlphaProject builds a lock-only project and installs it for
// claude, returning the project dir with skill "alpha" present at
// .claude/skills/alpha — the shared baseline for the remove/--force tests
// below.
func installedAlphaProject(t *testing.T) string {
	t.Helper()

	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude"); code != 0 {
		t.Fatalf("baseline install: code %d, stderr %q", code, stderr)
	}
	return dir
}

// TestRemoveCmd_NonInteractiveNoOptIn_AbortsWithoutChanges (spec 016 FR-001-
// 003, US1): a non-interactive `remove` with neither --force nor --yes must
// abort before touching the lockfile, agent dir, or store, and must name
// --force in its diagnostic.
func TestRemoveCmd_NonInteractiveNoOptIn_AbortsWithoutChanges(t *testing.T) {
	t.Parallel()

	dir := installedAlphaProject(t)
	agentDirsExist(t, dir, "claude")
	lockBefore, err := os.ReadFile(filepath.Join(dir, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read lock before remove: %v", err)
	}

	// A bytes.Buffer stdout is not a TTY, so runCLI is non-interactive by
	// construction (internal/cli/output.go NewOutput: interactive && isTTY).
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "remove", "alpha")

	if code != int(errs.CodeGeneric) {
		t.Errorf("code = %d, want %d (stdout=%q stderr=%q)", code, errs.CodeGeneric, stdout, stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("stderr = %q, want it to name --force", stderr)
	}
	lockAfter, err := os.ReadFile(filepath.Join(dir, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read lock after remove: %v", err)
	}
	if string(lockAfter) != string(lockBefore) {
		t.Errorf("skills-lock.json changed:\nbefore: %s\nafter:  %s", lockBefore, lockAfter)
	}
	agentDirsExist(t, dir, "claude")
}

// TestRemoveCmd_YesFlagIsEquivalentToForce (spec 016 Assumptions): the
// pre-existing global --yes flag satisfies the same opt-in requirement as
// --force.
func TestRemoveCmd_YesFlagIsEquivalentToForce(t *testing.T) {
	t.Parallel()

	dir := installedAlphaProject(t)

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "remove", "alpha", "--yes")
	if code != 0 {
		t.Fatalf("remove --yes: code %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha agent dir not removed (stat err=%v)", err)
	}
}

// TestRemoveCmd_InteractiveForceSkipsPrompt (spec 016 US3): at an
// interactive terminal, --force still skips the confirmation prompt, same
// as --yes does today.
func TestRemoveCmd_InteractiveForceSkipsPrompt(t *testing.T) {
	t.Parallel()

	dir := installedAlphaProject(t)

	var out, errb bytes.Buffer
	// Empty stdin: an unanswered prompt would default to "no", so removal
	// only succeeds here because --force skipped the prompt entirely.
	o := newInteractiveOutput(&out, &errb, "")
	c := removeCmd{Skills: []string{"alpha"}, Force: true}
	if err := c.Run(context.Background(), o, newTestApp(), projectRoot(dir), Globals{}); err != nil {
		t.Fatalf("Run: %v (stderr=%q)", err, errb.String())
	}
	if strings.Contains(errb.String(), "[y/N]") {
		t.Errorf("stderr = %q, want no confirmation prompt under --force", errb.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha agent dir not removed (stat err=%v)", err)
	}
}

// TestRemoveCmd_InteractiveNoOptIn_StillPrompts (spec 016 US3): at an
// interactive terminal with no opt-in flag, the y/N prompt still appears and
// still governs the outcome — this is the pre-existing decline path, not
// the new non-interactive guard, so nothing must change here.
func TestRemoveCmd_InteractiveNoOptIn_StillPrompts(t *testing.T) {
	t.Parallel()

	dir := installedAlphaProject(t)

	var out, errb bytes.Buffer
	o := newInteractiveOutput(&out, &errb, "n\n")
	c := removeCmd{Skills: []string{"alpha"}}
	err := c.Run(context.Background(), o, newTestApp(), projectRoot(dir), Globals{})
	if err == nil {
		t.Fatal("Run = nil error, want abort on decline")
	}
	if !strings.Contains(errb.String(), "[y/N]") {
		t.Errorf("stderr = %q, want the [y/N] prompt to have been shown", errb.String())
	}
	if strings.Contains(err.Error(), "--force") {
		t.Errorf("err = %q, want the pre-existing decline error, not the non-interactive --force guard", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "skills", "alpha")); statErr != nil {
		t.Errorf("alpha agent dir removed on decline: stat err=%v", statErr)
	}
}
