package cli

import (
	"bytes"
	"io"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/glapsfun/gskill/internal/tui"
)

// Styled --help for interactive terminals (design 2026-07-08). Kong's default
// printer renders into a buffer and a deterministic line transform colors it
// from the shared tui.Theme; without color (piped, NO_COLOR, dumb terminal)
// every style is a no-op, so the transform is a byte-for-byte identity and
// the help golden files stay authoritative. DocsModel never gets this
// printer, so generated docs and completion words are untouched.

// helpGroupTitles are the explicit command-group titles, shared between the
// kong grammar (grammarOptions) and the help classifier so they cannot drift.
var helpGroupTitles = []kong.Group{
	{Key: "core", Title: "CORE"},
	{Key: "inspect", Title: "INSPECT"},
	{Key: "project", Title: "PROJECT (lockfile · installed state)"},
	{Key: "more", Title: "MORE"},
}

// helpLineKind classifies one help line for styling.
type helpLineKind int

const (
	helpPlain      helpLineKind = iota
	helpUsage                   // the "Usage: gskill …" line
	helpSection                 // Flags:/Commands:/Arguments:/Examples: and group titles
	helpEntryRow                // a flag or argument row inside Flags:/Arguments:
	helpCommandRow              // a command row inside Commands:/a group section
	helpExample                 // a preformatted example line
)

// helpState tracks which section the classifier is inside.
type helpState int

const (
	helpStateNone helpState = iota
	helpStateEntries
	helpStateCommands
	helpStateExamples
)

// isHelpGroupTitle reports whether the line is one of the explicit group
// titles rendered by kong at column 0.
func isHelpGroupTitle(line string) bool {
	for _, g := range helpGroupTitles {
		if line == g.Title {
			return true
		}
	}
	return false
}

// helpSectionState maps a section-header line to the state it opens.
func helpSectionState(line string) (helpState, bool) {
	switch {
	case line == "Flags:" || line == "Arguments:":
		return helpStateEntries, true
	case line == "Commands:" || isHelpGroupTitle(line):
		return helpStateCommands, true
	case line == "Examples:":
		return helpStateExamples, true
	default:
		return helpStateNone, false
	}
}

// classifyHelpLine assigns a style kind to one line of kong help output and
// returns the section state for the next line.
func classifyHelpLine(state helpState, line string) (helpLineKind, helpState) {
	if strings.HasPrefix(line, "Usage: ") {
		return helpUsage, state
	}
	if next, ok := helpSectionState(line); ok {
		return helpSection, next
	}
	if line == "" {
		// Blank lines keep the section: kong separates entries with them.
		return helpPlain, state
	}
	if line[0] != ' ' {
		// A column-0 paragraph (description, trailing hint) ends the section.
		return helpPlain, helpStateNone
	}

	trimmed := strings.TrimLeft(line, " ")
	switch state {
	case helpStateEntries:
		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "<") {
			return helpEntryRow, state
		}
		return helpPlain, state // wrapped continuation of a help text
	case helpStateCommands:
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			return helpCommandRow, state
		}
		return helpPlain, state // the command's description line
	case helpStateExamples:
		return helpExample, state
	case helpStateNone:
		return helpPlain, state
	default:
		return helpPlain, state
	}
}

// styleHelp colors one rendered help page. Every rule preserves the exact
// characters of the line — only ANSI styling is added — so without color the
// function is an identity (locked by TestStyleHelp_IdentityWithoutColor).
func styleHelp(text string) string {
	st := tui.DefaultTheme()
	lines := strings.Split(text, "\n")
	state := helpStateNone
	for i, line := range lines {
		var kind helpLineKind
		kind, state = classifyHelpLine(state, line)
		switch kind {
		case helpUsage:
			if rest, ok := strings.CutPrefix(line, "Usage: gskill"); ok {
				lines[i] = "Usage: " + st.Accent.Render("gskill") + rest
			} // unexpected program name: leave untouched
		case helpSection:
			lines[i] = st.TableHeader.Render(line)
		case helpEntryRow:
			lines[i] = styleEntryRow(st, line)
		case helpCommandRow:
			lines[i] = styleCommandRow(st, line)
		case helpExample:
			lines[i] = st.Subtitle.Render(line)
		case helpPlain:
			// Unrecognized content always passes through unchanged.
		}
	}
	return strings.Join(lines, "\n")
}

// styleEntryRow colors the flag/argument tokens of a row, leaving the aligned
// description untouched. The head is everything before the first 2+ space gap
// after the tokens begin.
func styleEntryRow(st tui.Theme, line string) string {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	rest := line[indent:]
	if gap := strings.Index(rest, "  "); gap > 0 {
		return line[:indent] + st.Accent.Render(rest[:gap]) + rest[gap:]
	}
	return line[:indent] + st.Accent.Render(rest)
}

// styleCommandRow colors a command row's name token(s), leaving the summary
// suffix ("[flags]", argument placeholders) untouched.
func styleCommandRow(st tui.Theme, line string) string {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	rest := line[indent:]
	name, tail, found := strings.Cut(rest, " ")
	if !found {
		return line[:indent] + st.Accent.Render(rest)
	}
	return line[:indent] + st.Accent.Render(name) + " " + tail
}

// styledHelpPrinter returns kong's help printer for interactive runs: the
// default rendering, colored when stdout is a real terminal and prompts are
// enabled. --no-interactive is read from the parse context, not the grammar
// struct: kong prints help from the BeforeReset hook, before flag values are
// applied to the struct, so a root field would still be its zero value here.
func styledHelpPrinter(stdout io.Writer) kong.HelpPrinter {
	return func(options kong.HelpOptions, ctx *kong.Context) error {
		if !isTTY(stdout) || helpNoInteractive(ctx) {
			return kong.DefaultHelpPrinter(options, ctx)
		}
		var buf bytes.Buffer
		orig := ctx.Stdout
		ctx.Stdout = &buf
		err := kong.DefaultHelpPrinter(options, ctx)
		ctx.Stdout = orig
		if err != nil {
			return err
		}
		_, err = io.WriteString(orig, styleHelp(buf.String()))
		return err
	}
}

// helpNoInteractive reports whether --no-interactive was passed, reading the
// traced flag value (populated during parsing, so it is available inside the
// BeforeReset help hook).
func helpNoInteractive(ctx *kong.Context) bool {
	for _, f := range ctx.Flags() {
		if f.Name == "no-interactive" {
			v, _ := ctx.FlagValue(f).(bool)
			return v
		}
	}
	return false
}
