// Package cli is the gskill command-line view. It parses commands with Kong,
// renders human or JSON output through a shared harness, and translates errors
// into process exit codes. It depends only on the app service layer.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/glapsfun/gskill/internal/tui"
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
	stdin       io.Reader // confirmation replies; defaults to os.Stdin
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

// Confirm prompts on stderr for a yes/no answer read from stdin, defaulting
// to No. It returns true without prompting when assumeYes is set or the
// session is non-interactive, so unattended and CI runs never block (FR-012).
func (o *Output) Confirm(prompt string, assumeYes bool) bool {
	if assumeYes || !o.interactive {
		return true
	}
	in := o.stdin
	if in == nil {
		// A prompt can only be answered by a real terminal on stdin; with
		// stdin redirected or closed, proceed exactly as a non-interactive
		// run would instead of failing on the unanswerable read.
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return true
		}
		in = os.Stdin
	}
	_, _ = fmt.Fprintf(o.stderr, "%s [y/N]: ", styleDiag(o.stderrColor(), tui.DefaultTheme().Warning, prompt))
	var reply string
	_, _ = fmt.Fscanln(in, &reply)
	switch strings.ToLower(strings.TrimSpace(reply)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// Diag writes a diagnostic line to stderr unless quiet is set.
func (o *Output) Diag(format string, args ...any) {
	if o.quiet {
		return
	}
	_, _ = fmt.Fprintf(o.stderr, format+"\n", args...)
}

// styleDiag renders text in st when interactive, unchanged otherwise, so
// every Diag-based helper stays byte-identical without color (piped,
// NO_COLOR, --no-interactive) by construction.
func styleDiag(interactive bool, st lipgloss.Style, text string) string {
	if !interactive {
		return text
	}
	return st.Render(text)
}

// stderrColor reports whether diagnostics written to stderr should carry
// color. o.interactive alone answers "is stdout a TTY" (that is what
// NewOutput keys it on, correctly, for stdout-bound rendering) — it says
// nothing about whether stderr was independently redirected, e.g.
// `gskill add foo 2>err.log` with stdout still attached to a terminal. Every
// stderr-styling call site must check stderr's own TTY status too.
func (o *Output) stderrColor() bool {
	return o.interactive && isTTY(o.stderr)
}

// Info writes a neutral diagnostic to stderr, dimmed on an interactive
// terminal.
func (o *Output) Info(format string, args ...any) {
	o.Diag("%s", styleDiag(o.stderrColor(), tui.DefaultTheme().Subtitle, fmt.Sprintf(format, args...)))
}

// Warn writes a warning-severity diagnostic to stderr, yellow on an
// interactive terminal.
func (o *Output) Warn(format string, args ...any) {
	o.Diag("%s", styleDiag(o.stderrColor(), tui.DefaultTheme().Warning, fmt.Sprintf(format, args...)))
}

// ErrDiag writes an error-severity diagnostic to stderr, red on an
// interactive terminal. Unlike a command's returned error (which Run maps to
// an exit code), this is for error-severity lines a command reports without
// failing the whole run — and for Run's own error-reporting lines.
func (o *Output) ErrDiag(format string, args ...any) {
	o.Diag("%s", styleDiag(o.stderrColor(), tui.DefaultTheme().Error, fmt.Sprintf(format, args...)))
}

// Hint writes a dimmed, actionable follow-up line to stderr — the
// arrow-prefixed suggestion shown after an error (e.g. "→ run 'gskill
// doctor'").
func (o *Output) Hint(format string, args ...any) {
	o.Diag("%s", styleDiag(o.stderrColor(), tui.DefaultTheme().Hint, fmt.Sprintf(format, args...)))
}

// isTTY reports whether w is an interactive terminal.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
