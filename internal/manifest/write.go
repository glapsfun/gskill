package manifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
)

// Upsert writes skill's declaration into the manifest at path, replacing an
// existing block of the same name or appending a new one, and creating the file
// when absent.
//
// Writing is surgical — the file is spliced as text rather than re-marshalled
// from a struct — because a TOML round-trip discards comments and key order.
// The manifest is hand-authored and committed, so silently eating a user's
// comments would be a serious regression, not a cosmetic one.
func Upsert(path string, s Skill) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	block := renderSkill(s)

	// An existing declaration is rewritten where it stands, keeping the comment
	// that documents it. Re-appending it at the end instead would silently
	// delete that comment and shuffle the file on every `add` — the very
	// damage splicing as text exists to avoid.
	rest, at := dropSkillBlockAt(lines, s.Name)
	if at >= 0 {
		out := make([]string, 0, len(rest)+len(block)+1)
		out = append(out, rest[:at]...)
		out = append(out, block...)
		if tail := rest[at:]; len(tail) > 0 {
			if strings.TrimSpace(tail[0]) != "" {
				out = append(out, "")
			}
			out = append(out, tail...)
		}
		return writeLines(path, trimTrailingBlanks(out))
	}

	// Keep exactly one blank line between blocks.
	lines = trimTrailingBlanks(rest)
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, block...)
	return writeLines(path, lines)
}

// Remove deletes a skill's block and every `[skills.<name>.*]` sub-table.
// A missing manifest or absent block is a no-op, so `remove` stays idempotent.
func Remove(path, name string) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	return writeLines(path, dropSkillBlock(lines, name))
}

// dropSkillBlock removes the `[skills.<name>]` table, its sub-tables, and any
// comment block immediately preceding it — a comment sitting directly above a
// table documents that table, so leaving it orphaned would be worse than
// removing it.
func dropSkillBlock(lines []string, name string) []string {
	b := newBlockDropper(lines, name, false)
	for _, line := range lines {
		b.feed(line)
	}
	// Dropping the block leaves its blank separator on one side and the
	// removed comment's on the other; collapsing keeps the file looking
	// hand-written rather than accumulating a blank line per removal.
	return trimTrailingBlanks(collapseBlankRuns(b.finish()))
}

// collapseBlankRuns reduces every run of blank lines to a single one.
func collapseBlankRuns(lines []string) []string {
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, line)
	}
	return out
}

// dropSkillBlockAt removes the table but keeps its documenting comment, and
// reports the index in the returned slice where the block stood (-1 when the
// manifest did not declare it). Upsert splices the rewritten block back at that
// index, so an existing declaration keeps both its comment and its position.
func dropSkillBlockAt(lines []string, name string) ([]string, int) {
	b := newBlockDropper(lines, name, true)
	for _, line := range lines {
		b.feed(line)
	}
	out := b.finish()
	at := b.at
	if at > len(out) {
		at = len(out)
	}
	return out, at
}

func newBlockDropper(lines []string, name string, keepDoc bool) *blockDropper {
	return &blockDropper{
		header:  "[skills." + name + "]",
		prefix:  "[skills." + name + ".",
		keepDoc: keepDoc,
		at:      -1,
		out:     make([]string, 0, len(lines)),
	}
}

func trimTrailingBlanks(out []string) []string {
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// blockDropper filters a table and its sub-tables out of a TOML file while
// leaving every other byte alone. Comments and blanks encountered *while*
// skipping are held back rather than dropped: they may document the next
// table, and that is only knowable once the next header appears.
type blockDropper struct {
	header string
	prefix string
	// keepDoc keeps the comment above the table instead of removing it with
	// it: a rewrite (Upsert) puts the table straight back, a removal does not.
	keepDoc  bool
	out      []string
	pending  []string
	skipping bool
	// at is where in out the block began, or -1 when it was never seen.
	at int
}

func (b *blockDropper) feed(line string) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "[") {
		b.feedHeader(line, trimmed)
		return
	}
	if !b.skipping {
		b.out = append(b.out, line)
		return
	}
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		b.pending = append(b.pending, line)
		return
	}
	// A body line belongs to the skipped table, as do any blanks buffered
	// before it.
	b.pending = nil
}

func (b *blockDropper) feedHeader(line, trimmed string) {
	if trimmed == b.header || strings.HasPrefix(trimmed, b.prefix) {
		b.enterBlock()
		return
	}
	if b.skipping {
		b.out = append(b.out, b.pending...)
		b.pending = nil
		b.skipping = false
	}
	b.out = append(b.out, line)
}

// enterBlock marks the start of the table being filtered out. The comment
// above it goes with it on a removal, and stays on a rewrite, where the caller
// is about to splice the table straight back in.
func (b *blockDropper) enterBlock() {
	if !b.skipping {
		if !b.keepDoc {
			b.out = dropTrailingDoc(b.out)
		}
		if b.at < 0 {
			b.at = len(b.out)
		}
	}
	b.pending = nil
	b.skipping = true
}

func (b *blockDropper) finish() []string {
	b.out = append(b.out, b.pending...)
	b.pending = nil
	return b.out
}

// dropTrailingDoc removes a comment block (and one blank separator) that
// immediately precedes the table being dropped.
func dropTrailingDoc(out []string) []string {
	for len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "#") {
		out = out[:len(out)-1]
	}
	// Drop one blank line separating the comment block from the table.
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// renderSkill emits a skill's block deterministically: fixed key order, and
// sorted keys inside the replace map so map iteration order never reaches the
// file (Constitution I).
func renderSkill(s Skill) []string {
	lines := []string{"[skills." + s.Name + "]"}
	add := func(k, v string) {
		if v != "" {
			lines = append(lines, fmt.Sprintf("%s = %s", k, quote(v)))
		}
	}
	add("source", s.Source)
	add("skill", s.Skill)
	add("version", s.Version)
	add("ref", s.Ref)
	add("commit", s.Commit)
	if len(s.Agents) > 0 {
		lines = append(lines, "agents = "+quoteList(s.Agents))
	}
	add("mode", s.Mode)

	if s.Override.Empty() {
		return lines
	}
	lines = append(lines, "", "[skills."+s.Name+".override]")
	if len(s.Override.Replace) > 0 {
		targets := make([]string, 0, len(s.Override.Replace))
		for t := range s.Override.Replace {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		pairs := make([]string, 0, len(targets))
		for _, t := range targets {
			pairs = append(pairs, fmt.Sprintf("%s = %s", quote(t), quote(s.Override.Replace[t])))
		}
		lines = append(lines, "replace = { "+strings.Join(pairs, ", ")+" }")
	}
	for _, kv := range []struct {
		key  string
		list []string
	}{
		{"patch", s.Override.Patch},
		{"prepend", s.Override.Prepend},
		{"append", s.Override.Append},
	} {
		if len(kv.list) > 0 {
			lines = append(lines, kv.key+" = "+quoteList(kv.list))
		}
	}
	return lines
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func quoteList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, quote(s))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // caller supplies the project-root manifest path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeUsage, "read "+FileName, err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n"), nil
}

func writeLines(path string, lines []string) error {
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if err := fsutil.WriteFileAtomic(path, []byte(body), 0o644); err != nil {
		return errs.Wrap(errs.CodeUsage, "write "+FileName, err)
	}
	return nil
}

// SetKey rewrites exactly one key of one declaration in place (spec 024
// data-model.md §5): the value token is replaced and everything else on the
// line — indentation, spacing around "=", a trailing comment — survives, as
// does every other line of the file. A key the block lacks is inserted right
// after its source line. Upsert re-renders a whole block; upgrade must not,
// because a comment inside the block is the user's and a rewrite that ate it
// would be a regression dressed as a feature.
func SetKey(path, skill, key, value string) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	start, end := skillBlockBounds(lines, skill)
	if start < 0 {
		return errs.New(errs.CodeUsage, FileName+" declares no skill "+skill)
	}
	quoted := quote(value)
	for i := start + 1; i < end; i++ {
		indent, k, spacing, rest, ok := splitKeyLine(lines[i])
		if !ok || k != key {
			continue
		}
		_, tail := splitValueToken(rest)
		lines[i] = indent + key + spacing + quoted + tail
		return writeLines(path, lines)
	}
	at := start + 1
	for i := start + 1; i < end; i++ {
		if _, k, _, _, ok := splitKeyLine(lines[i]); ok && k == "source" {
			at = i + 1
			break
		}
	}
	inserted := make([]string, 0, len(lines)+1)
	inserted = append(inserted, lines[:at]...)
	inserted = append(inserted, key+" = "+quoted)
	inserted = append(inserted, lines[at:]...)
	return writeLines(path, inserted)
}

// skillBlockBounds returns the header index of `[skills.<name>]` and the index
// of the next table header (or len(lines)), or -1 when the block is absent.
func skillBlockBounds(lines []string, name string) (int, int) {
	header := "[skills." + name + "]"
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if trimmed == header {
				start = i
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			return start, i
		}
	}
	if start < 0 {
		return -1, -1
	}
	return start, len(lines)
}

// splitKeyLine splits `  key   = rest` into its parts, reporting false for
// blank, comment, and header lines.
func splitKeyLine(line string) (indent, key, spacing, rest string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	indent = line[:len(line)-len(trimmed)]
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return "", "", "", "", false
	}
	eq := strings.Index(trimmed, "=")
	if eq < 0 {
		return "", "", "", "", false
	}
	key = strings.TrimRight(trimmed[:eq], " \t")
	spacing = trimmed[len(key):eq+1] + leadingSpace(trimmed[eq+1:])
	rest = strings.TrimLeft(trimmed[eq+1:], " \t")
	return indent, key, spacing, rest, true
}

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// splitValueToken separates a TOML value from whatever follows it on the line
// (whitespace and an inline comment). Quoted strings honour escapes; any other
// value runs to the first "#" or end of line.
func splitValueToken(rest string) (value, tail string) {
	if strings.HasPrefix(rest, `"`) {
		for i := 1; i < len(rest); i++ {
			switch rest[i] {
			case '\\':
				i++
			case '"':
				return rest[:i+1], rest[i+1:]
			}
		}
		return rest, ""
	}
	if hash := strings.Index(rest, "#"); hash >= 0 {
		v := strings.TrimRight(rest[:hash], " \t")
		return v, rest[len(v):]
	}
	v := strings.TrimRight(rest, " \t")
	return v, rest[len(v):]
}
