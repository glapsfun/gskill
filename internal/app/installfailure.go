package app

import (
	"context"
	"errors"
	"io/fs"

	"github.com/glapsfun/gskill/internal/errs"
)

// FailureCategory is a normalized install-failure classification. Values are
// wire values in the --json document (contracts/install-result-json.md).
// Categories are derived from the typed errs sentinels via errors.Is/As —
// never by parsing error strings (spec 014 FR-013).
type FailureCategory string

// Failure categories (data-model.md classification table).
const (
	FailureSourceUnavailable FailureCategory = "source-unavailable"
	FailureAuthentication    FailureCategory = "authentication"
	FailureResolution        FailureCategory = "resolution"
	FailureInvalidMetadata   FailureCategory = "invalid-metadata"
	FailureUnsupportedSource FailureCategory = "unsupported-source"
	FailureUnsupportedAgent  FailureCategory = "unsupported-agent"
	FailureIntegrity         FailureCategory = "integrity"
	FailureStore             FailureCategory = "store"
	FailureFilesystem        FailureCategory = "filesystem"
	FailurePermission        FailureCategory = "permission"
	FailureForeignContent    FailureCategory = "foreign-content"
	FailureLink              FailureCategory = "link"
	FailureLockfile          FailureCategory = "lockfile"
	FailureCancelled         FailureCategory = "cancelled"
	FailureUnknown           FailureCategory = "unknown"
)

// errUnsupportedSourceType marks the unsupported-sourceType failure at its one
// construction site so classification stays sentinel-driven (FR-013).
var errUnsupportedSourceType = errors.New("unsupported source type")

// InstallFailure is the structured explanation of one skill's failure: what
// category of problem, in which phase, the complete message, and — when the
// error chain carries one — an actionable remediation hint. Message may
// contain untrusted remote text; renderers must sanitize it.
type InstallFailure struct {
	Category FailureCategory
	Phase    InstallPhase
	Message  string
	Hint     string
	Expected string // optional: recorded value (integrity mismatches)
	Actual   string // optional: observed value (integrity mismatches)
	Cause    error  // not serialized; kept for errors.Is/As in tests
}

// integrityMismatchError carries the recorded and computed hashes through the
// error chain so renderers get Expected/Actual as data, never by parsing the
// message (FR-013).
type integrityMismatchError struct {
	expected, actual string
	err              error
}

func (e *integrityMismatchError) Error() string { return e.err.Error() }
func (e *integrityMismatchError) Unwrap() error { return e.err }

// classifyFailure builds the structured failure for one skill: category from
// the typed error chain (refined by the failing phase), the complete message,
// and the chain's remediation hint. Returns nil for a nil error.
func classifyFailure(phase InstallPhase, err error) *InstallFailure {
	if err == nil {
		return nil
	}
	f := &InstallFailure{
		Category: categoryOf(phase, err),
		Phase:    phase,
		Message:  err.Error(),
		Hint:     errs.HintOf(err),
		Cause:    err,
	}
	if mm, ok := errors.AsType[*integrityMismatchError](err); ok {
		f.Expected, f.Actual = mm.expected, mm.actual
	}
	return f
}

// sentinelCategories maps typed sentinels to categories, checked in order:
// cancellation first so an aborted operation is never misreported as its own
// fault; ErrInvalidLock last among sentinels because it needs phase
// refinement (handled separately).
var sentinelCategories = []struct {
	is  error
	cat FailureCategory
}{
	{errs.ErrCancelled, FailureCancelled},
	{context.Canceled, FailureCancelled},
	{errUnsupportedSourceType, FailureUnsupportedSource},
	{errs.ErrIntegrity, FailureIntegrity},
	{errs.ErrAuth, FailureAuthentication},
	{errs.ErrSourceUnavailable, FailureSourceUnavailable},
	{errs.ErrUnsupportedAgent, FailureUnsupportedAgent},
	{errs.ErrCacheLock, FailureStore},
	{errs.ErrLockMismatch, FailureLockfile},
	// Usage errors surface per-skill when an entry cannot resolve its target
	// agents (e.g. a raw entry with no --agent); resolution is the closest
	// category — without this row they'd degrade to unknown despite being
	// fully typed.
	{errs.ErrUsage, FailureResolution},
}

// categoryOf maps a typed error chain plus the failing phase onto a category
// (data-model.md). Sentinels win over phase fallbacks.
func categoryOf(phase InstallPhase, err error) FailureCategory {
	for _, s := range sentinelCategories {
		if errors.Is(err, s.is) {
			return s.cat
		}
	}
	if errors.Is(err, errs.ErrInvalidLock) {
		return invalidLockCategory(phase)
	}
	if errors.Is(err, fs.ErrPermission) {
		return FailurePermission
	}
	switch phase { //nolint:exhaustive // remaining phases fall to unknown by design
	case InstallPhaseLinking:
		return FailureLink
	case InstallPhaseStoring, InstallPhaseCleaning:
		return FailureFilesystem
	case InstallPhaseResolving:
		return FailureResolution
	default:
		return FailureUnknown
	}
}

// invalidLockCategory refines ErrInvalidLock by the failing phase: metadata
// problems early, foreign/unmanaged content at linking, lock write problems
// at locking.
func invalidLockCategory(phase InstallPhase) FailureCategory {
	switch phase { //nolint:exhaustive // every earlier phase is a metadata problem
	case InstallPhaseLinking:
		return FailureForeignContent
	case InstallPhaseLocking:
		return FailureLockfile
	default:
		return FailureInvalidMetadata
	}
}
