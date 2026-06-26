package errs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
)

func TestExitCode_Nil(t *testing.T) {
	t.Parallel()

	if got := errs.ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", got)
	}
}

func TestExitCode_UnknownError(t *testing.T) {
	t.Parallel()

	if got := errs.ExitCode(errors.New("boom")); got != 1 {
		t.Errorf("ExitCode(unknown) = %d, want 1 (generic)", got)
	}
}

func TestExitCode_Sentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"usage", errs.ErrUsage, 2},
		{"invalid-manifest", errs.ErrInvalidManifest, 3},
		{"lock-mismatch", errs.ErrLockMismatch, 4},
		{"source-unavailable", errs.ErrSourceUnavailable, 5},
		{"integrity", errs.ErrIntegrity, 6},
		{"drift", errs.ErrDrift, 7},
		{"update-available", errs.ErrUpdateAvailable, 8},
		{"unsupported-agent", errs.ErrUnsupportedAgent, 9},
		{"partial-install", errs.ErrPartialInstall, 10},
		{"auth", errs.ErrAuth, 11},
		{"cache-lock", errs.ErrCacheLock, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := errs.ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestExitCode_WrappedSentinelPreservesCode(t *testing.T) {
	t.Parallel()

	// The authentication-failure (11) and partial-installation (10) codes must
	// survive %w wrapping (FR-038).
	authErr := fmt.Errorf("git fetch: %w", errs.ErrAuth)
	if got := errs.ExitCode(authErr); got != 11 {
		t.Errorf("ExitCode(wrapped auth) = %d, want 11", got)
	}

	partialErr := fmt.Errorf("install aborted midway: %w", errs.ErrPartialInstall)
	if got := errs.ExitCode(partialErr); got != 10 {
		t.Errorf("ExitCode(wrapped partial) = %d, want 10", got)
	}
}

func TestWrap_CarriesCodeAndCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection refused")
	err := errs.Wrap(errs.CodeSourceUnavailable, "clone failed", cause)

	if got := errs.ExitCode(err); got != 5 {
		t.Errorf("ExitCode(wrapped) = %d, want 5", got)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true (cause must remain unwrappable)")
	}
	if !errors.Is(err, errs.ErrSourceUnavailable) {
		t.Error("errors.Is(err, ErrSourceUnavailable) = false, want true (code must match sentinel)")
	}
}

func TestErrorsAs_FindsCodedError(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("wrapping: %w", errs.Wrap(errs.CodeIntegrity, "hash mismatch", nil))

	var coded *errs.Error
	if !errors.As(err, &coded) {
		t.Fatal("errors.As did not find *errs.Error in chain")
	}
	if coded.Code != errs.CodeIntegrity {
		t.Errorf("coded.Code = %d, want %d", coded.Code, errs.CodeIntegrity)
	}
}
