package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// newTestApp builds a real App with a discard logger, so alias runs exercise
// the same code paths as production invocations.
func newTestApp() *app.App {
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// runCLI executes the CLI in-process and captures both channels and the code.
func runCLI(t *testing.T, a *app.App, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errb bytes.Buffer
	code = Run(context.Background(), args, &out, &errb, a)
	return out.String(), errb.String(), code
}

// initedProject creates a fresh gskill project in a temp dir and returns it.
func initedProject(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "init")
	if code != 0 {
		t.Fatalf("init: exit code = %d, stderr: %q", code, stderr)
	}
	return dir
}

func TestAliasTable_EveryOldFormParses(t *testing.T) {
	t.Parallel()

	for _, m := range aliasTable {
		if m.Kind != aliasKindCommand {
			continue
		}
		t.Run(m.Old, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, code := runCLI(t, nil, m.Old, "--help")
			if code != 0 {
				t.Fatalf("gskill %s --help: exit code = %d, stderr: %q", m.Old, code, stderr)
			}
			if stdout == "" {
				t.Errorf("gskill %s --help produced no help output", m.Old)
			}
			if strings.Contains(strings.ToLower(stdout+stderr), "deprecat") {
				t.Errorf("gskill %s --help mentions deprecation; aliases must be silent", m.Old)
			}
		})
	}
}

func TestAliasTable_KongAliasesShareHelpByteIdentically(t *testing.T) {
	t.Parallel()

	for _, m := range aliasTable {
		if m.Kind != aliasKindCommand || m.Mechanism != aliasMechKong {
			continue
		}
		t.Run(m.Old, func(t *testing.T) {
			t.Parallel()

			oldOut, _, oldCode := runCLI(t, nil, m.Old, "--help")
			canonOut, _, canonCode := runCLI(t, nil, append(strings.Fields(m.Canonical), "--help")...)
			if oldCode != canonCode {
				t.Fatalf("exit codes differ: %s=%d %s=%d", m.Old, oldCode, m.Canonical, canonCode)
			}
			if oldOut != canonOut {
				t.Errorf("help output differs between %q and %q:\nold:  %q\ncanon: %q",
					m.Old, m.Canonical, oldOut, canonOut)
			}
		})
	}
}

func TestAliasTable_HiddenCommandsBehaveIdentically(t *testing.T) {
	t.Parallel()

	for _, m := range aliasTable {
		if m.Kind != aliasKindCommand || m.Mechanism != aliasMechHidden {
			continue
		}
		t.Run(m.Old, func(t *testing.T) {
			t.Parallel()

			// Two identical fresh projects, one per side, so a state-mutating
			// first run cannot skew the comparison.
			oldDir := initedProject(t)
			canonDir := initedProject(t)

			oldArgs := []string{"-C", oldDir, m.Old}
			canonArgs := append([]string{"-C", canonDir}, strings.Fields(m.Canonical)...)

			oldOut, oldErr, oldCode := runCLI(t, newTestApp(), oldArgs...)
			canonOut, canonErr, canonCode := runCLI(t, newTestApp(), canonArgs...)

			if oldCode != canonCode {
				t.Errorf("exit codes differ: `gskill %s`=%d `gskill %s`=%d",
					m.Old, oldCode, m.Canonical, canonCode)
			}
			if oldOut != canonOut {
				t.Errorf("stdout differs between `gskill %s` and `gskill %s`:\nold:  %q\ncanon: %q",
					m.Old, m.Canonical, oldOut, canonOut)
			}
			if oldErr != canonErr {
				t.Errorf("stderr differs between `gskill %s` and `gskill %s`:\nold:  %q\ncanon: %q",
					m.Old, m.Canonical, oldErr, canonErr)
			}
			if strings.Contains(strings.ToLower(oldOut+oldErr), "deprecat") {
				t.Errorf("`gskill %s` mentions deprecation; aliases must be silent", m.Old)
			}
		})
	}
}

func TestAliasTable_HiddenAliasesAbsentFromRootHelp(t *testing.T) {
	t.Parallel()

	stdout, _, code := runCLI(t, nil, "--help")
	if code != 0 {
		t.Fatalf("--help: exit code = %d", code)
	}
	for _, m := range aliasTable {
		if m.Mechanism != aliasMechHidden {
			continue
		}
		re := regexp.MustCompile(`(?m)^\s{2,4}` + m.Old + `\b`)
		if re.MatchString(stdout) {
			t.Errorf("hidden alias %q appears as a top-level entry in root help", m.Old)
		}
	}
}
