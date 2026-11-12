package docs

import (
	"path"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// invocation is one documented gskill command: where it was written and its
// arguments after the gskill word, ready for cli.ParseArgs.
type invocation struct {
	line  int
	text  string
	words []string
}

// shellFences are the fence tags whose contents are commands, not output.
var shellFences = map[string]bool{"sh": true, "bash": true, "shell": true, "console": true}

var (
	inlineCodeRE  = regexp.MustCompile("`([^`\n]+)`")
	placeholderRE = regexp.MustCompile(`<[A-Za-z][A-Za-z0-9_.-]*>`)
	envAssignRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// gskillInvocations returns every gskill command written in markdown: each
// command in a shell fence (sh, bash, shell, or the $ prompt lines of a
// console fence) and each inline code span that starts with gskill. Output
// fences, and the output lines of a console fence, are skipped.
func gskillInvocations(markdown string) []invocation {
	var sc markdownScanner
	for i, raw := range strings.Split(markdown, "\n") {
		row, trimmed := i+1, strings.TrimSpace(raw)
		switch {
		case sc.fence(trimmed):
		case sc.inFence && sc.isShell:
			sc.shellLine(row, trimmed)
		case !sc.inFence:
			sc.proseLine(row, raw)
		}
	}
	return sc.out
}

// markdownScanner is gskillInvocations' state across lines.
type markdownScanner struct {
	out        []invocation
	inFence    bool
	fenceMark  string
	isShell    bool
	promptOnly bool
	pending    strings.Builder
	startRow   int
}

// fence handles a line that opens or closes a fenced block and reports
// whether it did. Only a run of the opener's character, at least as long and
// with no info string, closes a block; anything else is block content.
func (sc *markdownScanner) fence(trimmed string) bool {
	mark := fenceRun(trimmed)
	switch {
	case mark == "":
		return false
	case !sc.inFence:
		tag := strings.TrimSpace(trimmed[len(mark):])
		sc.inFence, sc.fenceMark, sc.isShell, sc.promptOnly = true, mark, shellFences[tag], tag == "console"
		return true
	case mark[0] == sc.fenceMark[0] && len(mark) >= len(sc.fenceMark) && trimmed == mark:
		// A fence that closes on a trailing backslash still ends the command;
		// it must not merge into the next fence's first line.
		if sc.pending.Len() > 0 {
			sc.out = append(sc.out, commandInvocations(sc.startRow, sc.pending.String())...)
			sc.pending.Reset()
		}
		sc.inFence, sc.fenceMark, sc.isShell, sc.promptOnly = false, "", false, false
		return true
	}
	return false
}

func (sc *markdownScanner) shellLine(row int, trimmed string) {
	if sc.pending.Len() == 0 {
		if sc.promptOnly && !strings.HasPrefix(trimmed, "$ ") {
			return
		}
		sc.startRow = row
	}
	if strings.HasSuffix(trimmed, `\`) {
		sc.pending.WriteString(strings.TrimSuffix(trimmed, `\`) + " ")
		return
	}
	sc.pending.WriteString(trimmed)
	sc.out = append(sc.out, commandInvocations(sc.startRow, sc.pending.String())...)
	sc.pending.Reset()
}

func (sc *markdownScanner) proseLine(row int, raw string) {
	for _, m := range inlineCodeRE.FindAllStringSubmatch(raw, -1) {
		// A lone word in prose, such as `gskill`, names the binary rather than
		// running it.
		if strings.ContainsAny(strings.TrimSpace(m[1]), " \t") {
			sc.out = append(sc.out, commandInvocations(row, m[1])...)
		}
	}
}

// fenceRun returns the leading run of three or more backticks or tildes that
// opens or closes a fenced block, or "" when line is not a fence line.
func fenceRun(line string) string {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return ""
	}
	n := len(line) - len(strings.TrimLeft(line, line[:1]))
	if n < 3 {
		return ""
	}
	return line[:n]
}

// commandInvocations splits one logical shell line at its operators and keeps
// each segment whose command word is gskill (or a path ending in /gskill).
func commandInvocations(row int, line string) []invocation {
	line = strings.TrimPrefix(strings.TrimSpace(line), "$ ")
	line = placeholderRE.ReplaceAllString(line, "placeholder")
	var (
		out     []invocation
		segment []string
	)
	emit := func() {
		// `GSKILL_HOME=/tmp/x gskill list` still runs gskill.
		for len(segment) > 0 && envAssignRE.MatchString(segment[0]) {
			segment = segment[1:]
		}
		if len(segment) > 0 && path.Base(segment[0]) == "gskill" {
			out = append(out, invocation{line: row, text: strings.Join(segment, " "), words: segment[1:]})
		}
		segment = nil
	}
	for _, w := range shellWords(line) {
		if isShellOperator(w) {
			emit()
			continue
		}
		segment = append(segment, strings.TrimSuffix(strings.TrimPrefix(w, "["), "]"))
	}
	emit()
	return out
}

func isShellOperator(w string) bool {
	switch w {
	case "|", "||", "&", "&&", ";", ">", ">>":
		return true
	}
	return false
}

// shellWords splits a shell line into words, honoring single and double
// quotes and backslash escapes. Operators (| & ; >) come back as their own
// words, a redirected file descriptor (the 2 in 2>&1) is dropped, and an
// unquoted # that starts a word ends the line.
func shellWords(line string) []string {
	s := wordSplitter{rs: []rune(line)}
	for s.i < len(s.rs) && !s.done {
		if s.quote != 0 {
			s.quoted()
		} else {
			s.unquoted()
		}
		s.i++
	}
	s.flush()
	return s.words
}

// wordSplitter is shellWords' scanner state.
type wordSplitter struct {
	rs     []rune
	i      int
	words  []string
	cur    strings.Builder
	inWord bool
	quote  rune
	done   bool
}

func (s *wordSplitter) flush() {
	if s.inWord {
		s.words = append(s.words, s.cur.String())
	}
	s.cur.Reset()
	s.inWord = false
}

// next consumes and returns the rune after the current one.
func (s *wordSplitter) next() (rune, bool) {
	if s.i+1 >= len(s.rs) {
		return 0, false
	}
	s.i++
	return s.rs[s.i], true
}

func (s *wordSplitter) quoted() {
	r := s.rs[s.i]
	switch {
	case r == s.quote:
		s.quote = 0
	case r == '\\' && s.quote == '"':
		if escaped, ok := s.next(); ok {
			r = escaped
		}
		s.cur.WriteRune(r)
	default:
		s.cur.WriteRune(r)
	}
}

func (s *wordSplitter) unquoted() {
	r := s.rs[s.i]
	switch {
	case r == '\'' || r == '"':
		s.quote, s.inWord = r, true
	case r == '\\':
		if escaped, ok := s.next(); ok {
			r = escaped
		}
		s.cur.WriteRune(r)
		s.inWord = true
	case unicode.IsSpace(r):
		s.flush()
	case r == '#' && !s.inWord:
		s.flush()
		s.done = true
	case r == '|' || r == '&' || r == ';' || r == '>':
		s.operator(r)
	default:
		s.cur.WriteRune(r)
		s.inWord = true
	}
}

// operator emits r (doubled when repeated, as in && or >>) as its own word,
// dropping a file descriptor number written right before a redirect.
func (s *wordSplitter) operator(r rune) {
	if r == '>' && s.inWord && isDigits(s.cur.String()) {
		s.cur.Reset()
		s.inWord = false
	}
	s.flush()
	op := string(r)
	if s.i+1 < len(s.rs) && s.rs[s.i+1] == r {
		op += string(r)
		s.i++
	}
	s.words = append(s.words, op)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func TestShellWords(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		`gskill add ./skill --agent claude`:         {"gskill", "add", "./skill", "--agent", "claude"},
		`gskill add x --version '^1.0.0'`:           {"gskill", "add", "x", "--version", "^1.0.0"},
		`"$HOME/.local/bin/gskill" version`:         {"$HOME/.local/bin/gskill", "version"},
		`gskill list --json | jq .`:                 {"gskill", "list", "--json", "|", "jq", "."},
		`gskill list && gskill project verify`:      {"gskill", "list", "&&", "gskill", "project", "verify"},
		`gskill list 2>&1 > out.txt`:                {"gskill", "list", ">", "&", "1", ">", "out.txt"},
		`gskill update --list   # read-only report`: {"gskill", "update", "--list"},
		`gskill add "a b" --skill c\ d`:             {"gskill", "add", "a b", "--skill", "c d"},
		`echo "x # not a comment"; gskill version`:  {"echo", "x # not a comment", ";", "gskill", "version"},
		`gskill add x --include 'skills/*' --all`:   {"gskill", "add", "x", "--include", "skills/*", "--all"},
	}
	for in, want := range cases {
		if got := shellWords(in); !reflect.DeepEqual(got, want) {
			t.Errorf("shellWords(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGskillInvocations(t *testing.T) {
	t.Parallel()

	markdown := strings.Join([]string{
		"Run `gskill --json version` first. The `gskill` binary, the `gskill-install` skill and `npx @glapsfun/gskill version` are not commands to check.",
		"",
		"```sh",
		"$ gskill add github.com/acme/skills \\",
		"    --skill <name> --agent claude",
		"curl -sSfL https://example.com/x | sh",
		"gskill --json list | jq '.skills'",
		"```",
		"",
		"```text",
		"gskill output that is not a command",
		"```",
		"",
		"```bash",
		"gskill --json --dry-run self upgrade [--to X.Y.Z]",
		"```",
		"",
		"```sh",
		"gskill --json list \\",
		"```",
		"",
		"```sh",
		"gskill version",
		"```",
		"",
		"```console",
		"$ gskill --json version",
		"gskill v0.12.0 (printed output, not a command)",
		"```",
		"",
		"````markdown",
		"```sh",
		"gskill frobnicate",
		"```",
		"````",
		"",
		"```sh",
		"GSKILL_HOME=/tmp/x gskill list",
		"```",
	}, "\n")

	got := gskillInvocations(markdown)
	want := []invocation{
		{line: 1, text: "gskill --json version", words: []string{"--json", "version"}},
		{
			line: 4, text: "gskill add github.com/acme/skills --skill placeholder --agent claude",
			words: []string{"add", "github.com/acme/skills", "--skill", "placeholder", "--agent", "claude"},
		},
		{line: 7, text: "gskill --json list", words: []string{"--json", "list"}},
		{
			line: 15, text: "gskill --json --dry-run self upgrade --to X.Y.Z",
			words: []string{"--json", "--dry-run", "self", "upgrade", "--to", "X.Y.Z"},
		},
		{line: 19, text: "gskill --json list", words: []string{"--json", "list"}},
		{line: 23, text: "gskill version", words: []string{"version"}},
		{line: 27, text: "gskill --json version", words: []string{"--json", "version"}},
		{line: 38, text: "gskill list", words: []string{"list"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gskillInvocations:\n got  %+v\n want %+v", got, want)
	}
}

// TestCheckInvocationsReportsStaleCommands proves the stale-example guard
// fails, and says where, when a documented command no longer exists.
func TestCheckInvocationsReportsStaleCommands(t *testing.T) {
	t.Parallel()

	markdown := "Restore with `gskill install --frozen`.\n\n```sh\ngskill frobnicate\ngskill --json list\n```\n"
	problems, checked := checkInvocations("example.md", markdown)
	if checked != 3 {
		t.Errorf("checked %d commands, want 3", checked)
	}
	if len(problems) != 2 {
		t.Fatalf("problems = %q, want 2", problems)
	}
	for i, want := range []string{"example.md:1: gskill install --frozen:", "example.md:4: gskill frobnicate:"} {
		if !strings.HasPrefix(problems[i], want) {
			t.Errorf("problem %d = %q, want prefix %q", i, problems[i], want)
		}
	}
}
