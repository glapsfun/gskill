// Package errs defines gskill's typed errors and their mapping to process exit
// codes. Errors wrap their cause with %w so errors.Is and errors.As traverse
// the chain, and ExitCode resolves any error to a stable code in the range
// 0-12 documented in contracts/cli-commands.md.
package errs

import "errors"

// Code is a gskill process exit code.
type Code int

// Exit codes (FR-038). These are the process's external contract and must not
// be renumbered.
const (
	CodeOK                Code = 0  // success
	CodeGeneric           Code = 1  // generic/unexpected error
	CodeUsage             Code = 2  // usage error (bad flags/args)
	CodeInvalidManifest   Code = 3  // invalid manifest
	CodeLockMismatch      Code = 4  // lockfile mismatch (--frozen-lockfile would change)
	CodeSourceUnavailable Code = 5  // source unavailable / network
	CodeIntegrity         Code = 6  // integrity failure (checksum mismatch)
	CodeDrift             Code = 7  // drift detected (--fail-on-drift)
	CodeUpdateAvailable   Code = 8  // update available (outdated --exit-code)
	CodeUnsupportedAgent  Code = 9  // unsupported / undetected agent
	CodePartialInstall    Code = 10 // partial installation
	CodeAuth              Code = 11 // authentication failure
	CodeCacheLock         Code = 12 // cache / lock failure (incl. lock-acquire timeout)
)

// Error carries a gskill exit Code and, optionally, an underlying cause that
// remains reachable through errors.Is, errors.As, and errors.Unwrap, plus an
// optional Hint: a one-line actionable next step the CLI renders after the
// error message ("→ run 'gskill init' to create one"). Hint never changes
// Error() output, code mapping, or errors.Is/As matching.
type Error struct {
	Code Code
	Msg  string
	Err  error
	Hint string
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		if e.Msg == "" {
			return e.Err.Error()
		}
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

// Unwrap returns the wrapped cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// Is reports whether target is an *Error with the same Code, so a wrapped error
// matches the sentinel for its category.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.Code == t.Code
}

// Sentinel errors, one per exit code. Wrap them with %w (or Wrap) to attach
// context while preserving the code.
var (
	ErrUsage             = &Error{Code: CodeUsage, Msg: "usage error"}
	ErrInvalidManifest   = &Error{Code: CodeInvalidManifest, Msg: "invalid manifest"}
	ErrLockMismatch      = &Error{Code: CodeLockMismatch, Msg: "lockfile mismatch"}
	ErrSourceUnavailable = &Error{Code: CodeSourceUnavailable, Msg: "source unavailable"}
	ErrIntegrity         = &Error{Code: CodeIntegrity, Msg: "integrity failure"}
	ErrDrift             = &Error{Code: CodeDrift, Msg: "drift detected"}
	ErrUpdateAvailable   = &Error{Code: CodeUpdateAvailable, Msg: "update available"}
	ErrUnsupportedAgent  = &Error{Code: CodeUnsupportedAgent, Msg: "unsupported or undetected agent"}
	ErrPartialInstall    = &Error{Code: CodePartialInstall, Msg: "partial installation"}
	ErrAuth              = &Error{Code: CodeAuth, Msg: "authentication failure"}
	ErrCacheLock         = &Error{Code: CodeCacheLock, Msg: "cache or lock failure"}
)

// New returns an *Error carrying code and msg with no underlying cause.
func New(code Code, msg string) error {
	return &Error{Code: code, Msg: msg}
}

// Wrap returns an *Error carrying code and msg that wraps cause. cause may be
// nil, in which case the result behaves like New.
func Wrap(code Code, msg string, cause error) error {
	return &Error{Code: code, Msg: msg, Err: cause}
}

// WithHint returns err with an actionable next-step hint attached. The
// returned error reports the same message as err, resolves to the same exit
// code, and keeps the full chain reachable for errors.Is/As. A nil err
// returns nil.
func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	code := CodeGeneric
	var e *Error
	if errors.As(err, &e) {
		code = e.Code
	}
	return &Error{Code: code, Err: err, Hint: hint}
}

// HintOf returns the first non-empty hint found along err's chain, or "".
func HintOf(err error) string {
	for err != nil {
		var e *Error
		if !errors.As(err, &e) {
			return ""
		}
		if e.Hint != "" {
			return e.Hint
		}
		err = e.Unwrap()
	}
	return ""
}

// ExitCode resolves err to its process exit code: 0 for nil, the Code of the
// first *Error found in the chain, or CodeGeneric (1) for any other error.
func ExitCode(err error) int {
	if err == nil {
		return int(CodeOK)
	}
	var e *Error
	if errors.As(err, &e) {
		return int(e.Code)
	}
	return int(CodeGeneric)
}
