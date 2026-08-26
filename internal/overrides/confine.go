package overrides

import (
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
)

// resolveInSkill maps a skill-relative target onto the staged tree and proves
// it stays inside the skill, failing closed when it does not.
//
// This is the security boundary from research R2. A patch is a *committed*
// repository file, so it arrives with every clone: a diff naming
// `../../.claude/settings.json` would otherwise write into the agent's
// configuration on every teammate's machine. An override may only modify files
// within the skill it applies to — cross-skill and repo-wide edits are not
// expressible, by design.
//
// Confinement is checked against the real skill directory (symlinks included)
// rather than the staged copy, and *after* path resolution rather than by
// inspecting the string: only that defeats `..` segments, absolute paths, and
// symlinked escapes uniformly.
func resolveInSkill(skillDir, staged, target string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(target))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "~") {
		return "", confinementErr(target, "is absolute")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", confinementErr(target, "escapes the skill directory")
	}

	realSkill, err := filepath.EvalSymlinks(skillDir)
	if err != nil {
		realSkill = filepath.Clean(skillDir)
	}
	resolved := resolveExisting(filepath.Join(realSkill, clean))
	rel, relErr := filepath.Rel(realSkill, resolved)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", confinementErr(target, "resolves outside the skill directory")
	}

	return filepath.Join(staged, clean), nil
}

// resolveExisting resolves symlinks along as much of p as exists, so a
// symlinked *directory* pointing out of the skill is caught even when the leaf
// itself is absent.
func resolveExisting(p string) string {
	probe := p
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			return filepath.Join(resolved, strings.TrimPrefix(p, probe))
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return p
		}
		probe = parent
	}
}

// confinementErr fails closed with the integrity code: an override that tries
// to leave its skill is treated as a verification failure, not a usage slip,
// because the same declaration behaves identically whether it was authored
// locally or arrived in a clone.
func confinementErr(target, why string) error {
	return errs.WithHint(
		errs.Wrap(errs.CodeIntegrity, "override target "+target+" "+why, errs.ErrIntegrity),
		"an override may only modify files inside its own skill directory")
}
