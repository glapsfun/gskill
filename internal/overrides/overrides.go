// Package overrides applies a resolved override declaration to a skill's
// upstream content (spec 023): whole-file replacement, unified-diff patches,
// and prepend/append layering, in that fixed order.
//
// The pipeline is a deterministic function of its inputs — the same upstream
// content, declaration, and input files always produce the same bytes — which
// is what lets the result be committed and reproduced from a clone (FR-009).
package overrides

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
)

// skillFile is the layer target: prepend and append wrap the skill's entry
// document, which is the file an agent actually reads.
const skillFile = "SKILL.md"

// Spec is a resolved override declaration with repo-relative input paths.
type Spec struct {
	Replace map[string]string
	Patch   []string
	Prepend []string
	Append  []string
}

// Empty reports whether the declaration transforms nothing, in which case
// Apply leaves the tree untouched and the skill keeps its upstream hash.
func (s Spec) Empty() bool {
	return len(s.Replace) == 0 && len(s.Patch) == 0 && len(s.Prepend) == 0 && len(s.Append) == 0
}

// Apply transforms skillDir in place, resolving override inputs against
// repoRoot. Stages run in the fixed order replace → patch → layer (FR-004):
// replace establishes the file that ships, patch adjusts that file, and
// layering goes last so appended content is never itself patched.
//
// The whole operation is all-or-nothing (FR-011). Work happens on a staged
// copy and is swapped in only after every stage succeeds, so a patch that
// stops applying leaves the previously materialized content exactly as it was
// — never half-transformed.
func Apply(skillDir, repoRoot string, spec Spec) error {
	if spec.Empty() {
		return nil
	}

	stage, err := os.MkdirTemp(filepath.Dir(skillDir), ".gskill-override-")
	if err != nil {
		return errs.Wrap(errs.CodeGeneric, "stage override", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	staged := filepath.Join(stage, filepath.Base(skillDir))
	if err := fsutil.CopyDir(skillDir, staged); err != nil {
		return errs.Wrap(errs.CodeGeneric, "stage override", err)
	}

	for _, stage := range []func(string, string, string, Spec) error{
		applyReplace,
		applyPatches,
		applyLayers,
	} {
		if err := stage(staged, skillDir, repoRoot, spec); err != nil {
			return err
		}
	}

	return swapIn(skillDir, staged)
}

// applyReplace performs whole-file swaps. Targets are visited in sorted order
// so map iteration order never influences the result (Constitution I).
func applyReplace(staged, skillDir, repoRoot string, spec Spec) error {
	targets := make([]string, 0, len(spec.Replace))
	for t := range spec.Replace {
		targets = append(targets, t)
	}
	sort.Strings(targets)

	for _, target := range targets {
		dst, err := resolveInSkill(skillDir, staged, target)
		if err != nil {
			return err
		}
		data, err := readInput(repoRoot, spec.Replace[target])
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return errs.Wrap(errs.CodeGeneric, "replace "+target, err)
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return errs.Wrap(errs.CodeGeneric, "replace "+target, err)
		}
	}
	return nil
}

// applyLayers prepends and appends content around SKILL.md, in declared order.
func applyLayers(staged, skillDir, repoRoot string, spec Spec) error {
	if len(spec.Prepend) == 0 && len(spec.Append) == 0 {
		return nil
	}
	dst, err := resolveInSkill(skillDir, staged, skillFile)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(dst) //nolint:gosec // dst is confined to the staged skill directory
	if err != nil {
		return errs.Wrap(errs.CodeIntegrity, "layer onto "+skillFile, err)
	}

	var out []byte
	for _, rel := range spec.Prepend {
		data, err := readInput(repoRoot, rel)
		if err != nil {
			return err
		}
		out = append(out, ensureTrailingNewline(data)...)
	}
	out = append(out, ensureTrailingNewline(body)...)
	for _, rel := range spec.Append {
		data, err := readInput(repoRoot, rel)
		if err != nil {
			return err
		}
		out = append(out, ensureTrailingNewline(data)...)
	}

	if err := os.WriteFile(dst, out, 0o600); err != nil {
		return errs.Wrap(errs.CodeGeneric, "layer onto "+skillFile, err)
	}
	return nil
}

// ensureTrailingNewline keeps concatenation from gluing a layer's last line to
// the next fragment's first, which would silently corrupt the result.
func ensureTrailingNewline(data []byte) []byte {
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return data
	}
	return append(append([]byte{}, data...), '\n')
}

// readInput reads a repo-relative override input file.
func readInput(repoRoot, rel string) ([]byte, error) {
	p := filepath.Join(repoRoot, filepath.FromSlash(rel))
	data, err := os.ReadFile(p) //nolint:gosec // rel is validated against the repo before this point
	if err != nil {
		return nil, errs.Wrap(errs.CodeUsage, "read override input "+rel, err)
	}
	return data, nil
}

// swapIn replaces skillDir's contents with the staged result.
func swapIn(skillDir, staged string) error {
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return errs.Wrap(errs.CodeGeneric, "apply override", err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(skillDir, e.Name())); err != nil {
			return errs.Wrap(errs.CodeGeneric, "apply override", err)
		}
	}
	stagedEntries, err := os.ReadDir(staged)
	if err != nil {
		return errs.Wrap(errs.CodeGeneric, "apply override", err)
	}
	for _, e := range stagedEntries {
		src := filepath.Join(staged, e.Name())
		dst := filepath.Join(skillDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return errs.Wrap(errs.CodeGeneric, "apply override", err)
		}
	}
	return nil
}
