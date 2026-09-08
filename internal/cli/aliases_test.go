package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	if _, err := newTestApp().Init(context.Background(), dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	return dir
}

// assertNoLocalProjectState fails the test unless dir has none of the local
// project state (.gskill, .agents, .gitignore) that Init creates — the
// precondition for characterizing auto-init from a virgin directory (spec
// 017 FR-001/FR-002).
func assertNoLocalProjectState(t *testing.T, dir string) {
	t.Helper()
	for _, missing := range []string{".gskill", ".agents", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, missing)); !os.IsNotExist(err) {
			t.Fatalf("precondition failed: %s already exists", missing)
		}
	}
}

// assertLocalProjectStateCreated fails the test unless .gskill and .agents
// exist as directories under dir and .gitignore contains both managed
// patterns (spec 017 FR-001/FR-002/FR-004).
func assertLocalProjectStateCreated(t *testing.T, dir string) {
	t.Helper()
	for _, want := range []string{".gskill", ".agents"} {
		info, err := os.Stat(filepath.Join(dir, want))
		if err != nil {
			t.Errorf("%s was not created: %v", want, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", want)
		}
	}
	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf(".gitignore was not created: %v", err)
	}
	if !strings.Contains(string(gitignore), ".gskill/") {
		t.Errorf(".gitignore missing %q:\n%s", ".gskill/", gitignore)
	}
	// Spec 022: skill content is committed — .agents/ must NOT be ignored.
	if strings.Contains(string(gitignore), ".agents/") {
		t.Errorf(".gitignore must not ignore .agents/:\n%s", gitignore)
	}
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
