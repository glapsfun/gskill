package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
)

// Classification is sentinel-driven (errors.Is/As) and phase-refined — never
// string parsing (spec 014 FR-013, research.md Decision 2).
func TestClassifyFailure_Categories(t *testing.T) {
	t.Parallel()

	wrap := func(sentinel error) error {
		return fmt.Errorf("skill %q: %w", "alpha", fmt.Errorf("context: %w", sentinel))
	}

	tests := []struct {
		name  string
		phase InstallPhase
		err   error
		want  FailureCategory
	}{
		{"integrity", InstallPhaseVerifying, wrap(errs.ErrIntegrity), FailureIntegrity},
		{"authentication", InstallPhaseFetching, wrap(errs.ErrAuth), FailureAuthentication},
		{"source-unavailable", InstallPhaseFetching, wrap(errs.ErrSourceUnavailable), FailureSourceUnavailable},
		{"unsupported-agent", "", wrap(errs.ErrUnsupportedAgent), FailureUnsupportedAgent},
		{"store", InstallPhaseStoring, wrap(errs.ErrCacheLock), FailureStore},
		{"cancelled-sentinel", InstallPhaseFetching, wrap(errs.ErrCancelled), FailureCancelled},
		{"cancelled-context", InstallPhaseFetching, wrap(context.Canceled), FailureCancelled},
		{"permission", InstallPhaseLinking, wrap(fs.ErrPermission), FailurePermission},
		{"unsupported-source", "", wrap(errUnsupportedSourceType), FailureUnsupportedSource},
		// ErrInvalidLock refines by phase: metadata problems early,
		// foreign/unmanaged content at linking, lock write problems at locking.
		{"invalid-metadata", InstallPhaseReadingMetadata, wrap(errs.ErrInvalidLock), FailureInvalidMetadata},
		{"foreign-content", InstallPhaseLinking, wrap(errs.ErrInvalidLock), FailureForeignContent},
		{"lockfile", InstallPhaseLocking, wrap(errs.ErrInvalidLock), FailureLockfile},
		{"lockfile-mismatch", "", wrap(errs.ErrLockMismatch), FailureLockfile},
		{"usage-as-resolution", "", wrap(errs.ErrUsage), FailureResolution},
		// Phase-refined fallbacks for errors matching no sentinel.
		{"link", InstallPhaseLinking, errors.New("symlink boom"), FailureLink},
		{"filesystem-storing", InstallPhaseStoring, errors.New("disk full"), FailureFilesystem},
		{"filesystem-cleaning", InstallPhaseCleaning, errors.New("rm boom"), FailureFilesystem},
		{"resolution", InstallPhaseResolving, errors.New("no such ref v9.9.9"), FailureResolution},
		{"unknown", "", errors.New("mystery"), FailureUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := classifyFailure(tt.phase, tt.err)
			if f == nil {
				t.Fatal("classifyFailure returned nil for a non-nil error")
			}
			if f.Category != tt.want {
				t.Errorf("Category = %q, want %q", f.Category, tt.want)
			}
			if f.Phase != tt.phase {
				t.Errorf("Phase = %q, want %q", f.Phase, tt.phase)
			}
			if f.Message != tt.err.Error() {
				t.Errorf("Message = %q, want the complete error text %q", f.Message, tt.err.Error())
			}
			if f.Cause == nil || !errors.Is(f.Cause, tt.err) {
				t.Error("Cause does not preserve the original chain")
			}
		})
	}
}

func TestClassifyFailure_NilError(t *testing.T) {
	t.Parallel()
	if f := classifyFailure(InstallPhaseVerifying, nil); f != nil {
		t.Errorf("classifyFailure(nil) = %+v, want nil", f)
	}
}

// The remediation hint travels from the errs chain (errs.HintOf), so the
// fail-closed construction sites' existing hints (e.g. the --force integrity
// hint) surface without re-stating them.
func TestClassifyFailure_HintFromChain(t *testing.T) {
	t.Parallel()

	err := errs.WithHint(
		fmt.Errorf("%w: computedHash mismatch", errs.ErrIntegrity),
		"re-run with --force to accept the changed upstream content")
	f := classifyFailure(InstallPhaseVerifying, err)
	if f.Hint != "re-run with --force to accept the changed upstream content" {
		t.Errorf("Hint = %q, want the chain's hint", f.Hint)
	}

	if f := classifyFailure(InstallPhaseVerifying, errors.New("bare")); f.Hint != "" {
		t.Errorf("Hint = %q for a hintless chain, want empty", f.Hint)
	}
}
