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
	lines = dropSkillBlock(lines, s.Name)
	block := renderSkill(s)

	// Keep exactly one blank line between blocks.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
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
	b := blockDropper{
		header: "[skills." + name + "]",
		prefix: "[skills." + name + ".",
		out:    make([]string, 0, len(lines)),
	}
	for _, line := range lines {
		b.feed(line)
	}
	return b.finish()
}

// blockDropper filters a table and its sub-tables out of a TOML file while
// leaving every other byte alone. Comments and blanks encountered *while*
// skipping are held back rather than dropped: they may document the next
// table, and that is only knowable once the next header appears.
type blockDropper struct {
	header   string
	prefix   string
	out      []string
	pending  []string
	skipping bool
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
		if !b.skipping {
			// Entering the block: its own preceding comment goes with it.
			b.out = dropTrailingDoc(b.out)
		}
		b.pending = nil
		b.skipping = true
		return
	}
	if b.skipping {
		b.out = append(b.out, b.pending...)
		b.pending = nil
		b.skipping = false
	}
	b.out = append(b.out, line)
}

func (b *blockDropper) finish() []string {
	b.out = append(b.out, b.pending...)
	b.pending = nil
	out := b.out
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// dropTrailingDoc removes a comment block (and one blank separator) that
// immediately precedes the table being dropped.
func dropTrailingDoc(out []string) []string {
	for len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "#") {
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
