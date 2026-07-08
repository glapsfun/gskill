package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
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

// TestHelpNoInteractive_ReadsParsedFlag proves --no-interactive reaches the
// help printer: kong prints help from the BeforeReset hook, before flag
// values are applied to the grammar struct, so the printer must read the
// traced value from the context instead of the struct field.
func TestHelpNoInteractive_ReadsParsedFlag(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"--no-interactive", "--help"}, true},
		{[]string{"--help", "--no-interactive"}, true},
		{[]string{"--help"}, false},
	} {
		var root rootCLI
		var got, printed bool
		options := append(grammarOptions(),
			kong.Writers(io.Discard, io.Discard),
			kong.Exit(func(int) {}),
			kong.Help(func(_ kong.HelpOptions, ctx *kong.Context) error {
				got, printed = helpNoInteractive(ctx), true
				return nil
			}),
		)
		parser, err := kong.New(&root, options...)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = parser.Parse(tc.args)
		if !printed {
			t.Fatalf("%v: help printer never ran", tc.args)
		}
		if got != tc.want {
			t.Errorf("helpNoInteractive with %v = %v, want %v", tc.args, got, tc.want)
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
