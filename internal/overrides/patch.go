package overrides

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"

	"github.com/glapsfun/gskill/internal/errs"
)

// applyPatches applies unified diffs in declared order, so a later patch sees
// the earlier one's result.
//
// Application is strict — exact context, zero fuzz (research R1). Strictness is
// what makes FR-011 unambiguous: "does not apply cleanly" is always an error
// with a named cause, never a silently fuzzy match that produces content the
// author never wrote. The diff never chooses its own write location: gskill
// extracts each target path, confines it to the skill, and performs the write.
func applyPatches(staged, skillDir, repoRoot string, spec Spec) error {
	for _, rel := range spec.Patch {
		data, err := readInput(repoRoot, rel)
		if err != nil {
			return err
		}
		files, _, err := gitdiff.Parse(bytes.NewReader(data))
		if err != nil {
			return patchErr(rel, "is not a valid unified diff", err)
		}
		for _, f := range files {
			if err := applyFragment(staged, skillDir, rel, f); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyFragment(staged, skillDir, patchRel string, f *gitdiff.File) error {
	target := stripDiffPrefix(f.NewName)
	if target == "" {
		target = stripDiffPrefix(f.OldName)
	}
	if target == "" {
		return patchErr(patchRel, "does not name a target file", nil)
	}

	dst, err := resolveInSkill(skillDir, staged, target)
	if err != nil {
		return err
	}

	current, err := os.ReadFile(dst) //nolint:gosec // dst is confined to the staged skill directory
	if err != nil {
		return patchErr(patchRel, "targets "+target+", which the skill does not contain", err)
	}

	var out bytes.Buffer
	if err := gitdiff.Apply(&out, bytes.NewReader(current), f); err != nil {
		return patchErr(patchRel, "no longer applies to "+target+
			" (the upstream content changed under it)", err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return patchErr(patchRel, "could not be written", err)
	}
	if err := os.WriteFile(dst, out.Bytes(), 0o600); err != nil {
		return patchErr(patchRel, "could not be written", err)
	}
	return nil
}

// patchErr reports a patch failure as an integrity failure (exit 6): the
// recorded inputs no longer produce the recorded output. Nothing is written,
// because the whole pipeline runs on a staged copy that is discarded on error.
func patchErr(patchRel, why string, cause error) error {
	return errs.WithHint(
		errs.Wrap(errs.CodeIntegrity, "patch "+patchRel+" "+why, cause),
		"re-generate the patch against the current upstream, or remove it from the manifest")
}

// stripDiffPrefix removes git's conventional a/ or b/ path prefix, equivalent
// to `git apply -p1`. Exactly one component is stripped: doing it blindly would
// let "b/../../etc/x" shed its escape and look innocent, so the remainder is
// still handed to resolveInSkill for confinement.
func stripDiffPrefix(name string) string {
	switch {
	case strings.HasPrefix(name, "a/"):
		return name[2:]
	case strings.HasPrefix(name, "b/"):
		return name[2:]
	default:
		return name
	}
}
