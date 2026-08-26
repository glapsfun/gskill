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
	target := f.NewName
	if target == "" {
		target = f.OldName
	}
	if target == "" {
		return patchErr(patchRel, "does not name a target file", nil)
	}

	dst, target, err := resolvePatchTarget(staged, skillDir, target)
	if err != nil {
		return err
	}

	// A creation diff has nothing to read: it applies against empty content.
	// Anything else must find its target, or the patch was written for a
	// different tree than the one being customized.
	var current []byte
	if !f.IsNew {
		var readErr error
		current, readErr = os.ReadFile(dst) //nolint:gosec // dst is confined to the staged skill directory
		if readErr != nil {
			return patchErr(patchRel, "targets "+target+", which the skill does not contain", readErr)
		}
	}

	var out bytes.Buffer
	if err := gitdiff.Apply(&out, bytes.NewReader(current), f); err != nil {
		return patchErr(patchRel, "no longer applies to "+target+
			" (the upstream content changed under it)", err)
	}

	// A deletion diff removes the file. Writing the empty result instead would
	// leave a zero-byte file the patch author meant to be gone.
	if f.IsDelete {
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return patchErr(patchRel, "could not delete "+target, err)
		}
		return nil
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

// resolvePatchTarget maps a diff's declared path onto the staged tree,
// returning the confined destination and the name to report in errors.
//
// The name is taken as declared first and only then with a leading a/ or b/
// stripped, because the two diff dialects disagree: a `diff --git` header is
// parsed with its prefix already removed, while a plain unified diff keeps it.
// Stripping unconditionally would mangle a skill whose own top-level directory
// is named "a" or "b" — its git patch of `b/notes.md` would be applied to
// `notes.md` instead. Preferring the path that actually exists resolves both
// dialects without guessing.
//
// Exactly one component is ever stripped, and the result is always handed to
// resolveInSkill: "b/../../etc/x" must not shed its escape and look innocent.
func resolvePatchTarget(staged, skillDir, name string) (string, string, error) {
	dst, err := resolveInSkill(skillDir, staged, name)
	if err == nil {
		// For modification diffs, the file must already exist.
		if _, statErr := os.Lstat(dst); statErr == nil {
			return dst, name, nil
		}
		// For creation diffs, the target file may not exist yet; treat an
		// existing parent directory as evidence the declared path is real.
		if _, dirErr := os.Stat(filepath.Dir(dst)); dirErr == nil {
			return dst, name, nil
		}
	}
	stripped := stripDiffPrefix(name)
	if stripped == name || stripped == "" {
		if err != nil {
			return "", name, err
		}
		return dst, name, nil
	}
	strippedDst, strippedErr := resolveInSkill(skillDir, staged, stripped)
	if strippedErr != nil {
		return "", stripped, strippedErr
	}
	return strippedDst, stripped, nil
}

// stripDiffPrefix removes git's conventional a/ or b/ path prefix, equivalent
// to `git apply -p1`.
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
