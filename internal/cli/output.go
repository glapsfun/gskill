// Package cli is the gskill command-line view. It parses commands with Kong,
// renders human or JSON output through a shared harness, and translates errors
// into process exit codes. It depends only on the app service layer.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// OutputOptions configures an Output harness.
type OutputOptions struct {
	JSON        bool
	Quiet       bool
	Interactive bool
}

// Output renders primary results to stdout and diagnostics to stderr, keeping
// the two channels separate so --json stdout stays machine-parseable (FR-035,
// FR-036).
type Output struct {
	stdout      io.Writer
	stderr      io.Writer
	json        bool
	quiet       bool
	interactive bool
}

// NewOutput builds an Output. Interactive is forced off when stdout is not a
// TTY, so non-interactive (CI-safe) behavior is the default outside a terminal.
func NewOutput(stdout, stderr io.Writer, opts OutputOptions) *Output {
	return &Output{
		stdout:      stdout,
		stderr:      stderr,
		json:        opts.JSON,
		quiet:       opts.Quiet,
		interactive: opts.Interactive && isTTY(stdout),
	}
}

// JSON reports whether JSON output is enabled.
func (o *Output) JSON() bool { return o.json }

// Interactive reports whether interactive UI (prompts, colors) is enabled.
func (o *Output) Interactive() bool { return o.interactive }

// Result writes the primary command result to stdout. In JSON mode it encodes v
// as a single indented top-level object; otherwise it writes human.
func (o *Output) Result(human string, v any) error {
	if o.json {
		enc := json.NewEncoder(o.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return fmt.Errorf("encode json result: %w", err)
		}
		return nil
	}
	if _, err := fmt.Fprintln(o.stdout, human); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}

// Diag writes a diagnostic line to stderr unless quiet is set.
func (o *Output) Diag(format string, args ...any) {
	if o.quiet {
		return
	}
	_, _ = fmt.Fprintf(o.stderr, format+"\n", args...)
}

// isTTY reports whether w is an interactive terminal.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
