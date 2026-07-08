package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyHelpLines(t *testing.T) {
	t.Parallel()

	sample := []string{
		"Usage: gskill add <source> [flags]",
		"",
		"Add and install a new skill.",
		"",
		"Examples:",
		"",
		"    gskill add github.com/owner/repo",
		"",
		"Arguments:",
		"  <source>    Skill source: git shorthand, URL, or local path.",
		"",
		"Flags:",
		"  -h, --help              Show context-sensitive help.",
		"                          continuation of the help text.",
		"",
		"CORE",
		"  init [flags]",
		"    Scaffold a gskill project (manifest, state dir, gitignore).",
		"",
		`Run "gskill <command> --help" for more information on a command.`,
	}
	want := []helpLineKind{
		helpUsage,
		helpPlain,
		helpPlain,
		helpPlain,
		helpSection,
		helpPlain,
		helpExample,
		helpPlain,
		helpSection,
		helpEntryRow, // <source> argument row
		helpPlain,
		helpSection,
		helpEntryRow, // flag row
		helpPlain,    // continuation, not a flag
		helpPlain,
		helpSection, // CORE group title
		helpCommandRow,
		helpPlain, // command description line (4-space indent)
		helpPlain,
		helpPlain, // trailing hint
	}

	state := helpStateNone
	for i, line := range sample {
		var kind helpLineKind
		kind, state = classifyHelpLine(state, line)
		if kind != want[i] {
			t.Errorf("line %d %q: kind = %v, want %v", i, line, kind, want[i])
		}
	}
}

// TestStyleHelp_IdentityWithoutColor proves the transform can never mangle
// help text: with the test environment's colorless profile every style is a
// no-op, so styling any locked help page must return it byte-for-byte.
func TestStyleHelp_IdentityWithoutColor(t *testing.T) {
	t.Parallel()

	goldens, err := filepath.Glob(filepath.Join("testdata", "help", "*.golden"))
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no golden help pages found: %v", err)
	}
	for _, path := range goldens {
		raw, err := os.ReadFile(path) //nolint:gosec // fixed glob under testdata/help
		if err != nil {
			t.Fatal(err)
		}
		if got := styleHelp(string(raw)); got != string(raw) {
			t.Errorf("%s: styleHelp is not an identity without color", filepath.Base(path))
			for i, l := range strings.Split(got, "\n") {
				orig := strings.Split(string(raw), "\n")
				if i < len(orig) && l != orig[i] {
					t.Errorf("  first divergence at line %d: %q != %q", i, l, orig[i])
					break
				}
			}
		}
	}
}
