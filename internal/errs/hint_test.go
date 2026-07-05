package errs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
)

func TestWithHint_PreservesMessageCodeAndChain(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("%w: no gskill.toml found", errs.ErrInvalidManifest)
	hinted := errs.WithHint(cause, "run 'gskill init' to create one")

	if hinted.Error() != cause.Error() {
		t.Errorf("WithHint changed Error(): got %q, want %q", hinted.Error(), cause.Error())
	}
	if got := errs.ExitCode(hinted); got != 3 {
		t.Errorf("ExitCode(hinted) = %d, want 3 (invalid manifest)", got)
	}
	if !errors.Is(hinted, errs.ErrInvalidManifest) {
		t.Error("errors.Is lost the sentinel through WithHint")
	}
	if got := errs.HintOf(hinted); got != "run 'gskill init' to create one" {
		t.Errorf("HintOf = %q, want the attached hint", got)
	}
}

func TestWithHint_GenericErrorGetsGenericCode(t *testing.T) {
	t.Parallel()

	hinted := errs.WithHint(errors.New("boom"), "try again")
	if got := errs.ExitCode(hinted); got != 1 {
		t.Errorf("ExitCode = %d, want 1 (generic)", got)
	}
	if got := errs.HintOf(hinted); got != "try again" {
		t.Errorf("HintOf = %q, want %q", got, "try again")
	}
}

func TestHintOf_NoHint(t *testing.T) {
	t.Parallel()

	if got := errs.HintOf(errs.ErrDrift); got != "" {
		t.Errorf("HintOf(unhinted sentinel) = %q, want empty", got)
	}
	if got := errs.HintOf(nil); got != "" {
		t.Errorf("HintOf(nil) = %q, want empty", got)
	}
}

func TestHintOf_FindsHintThroughOuterWrapping(t *testing.T) {
	t.Parallel()

	hinted := errs.WithHint(errs.ErrLockMismatch, "run 'gskill project lock' to recompute the lockfile")
	rewrapped := fmt.Errorf("install: %w", hinted)

	if got := errs.HintOf(rewrapped); got != "run 'gskill project lock' to recompute the lockfile" {
		t.Errorf("HintOf(rewrapped) = %q, want inner hint", got)
	}
	if got := errs.ExitCode(rewrapped); got != 4 {
		t.Errorf("ExitCode(rewrapped) = %d, want 4", got)
	}
}
