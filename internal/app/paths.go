package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
)

// safeTargetForRemoval returns the gskill-managed path to delete for an agent's
// target, derived from the registered adapter (trusted) and cross-checked against
// the lockfile-recorded path (untrusted input — the committed lockfile). It
// returns ok=false when the agent is unknown, the recorded path does not match
// the adapter-derived location, or the derived path escapes its expected root, so
// a malformed or malicious lockfile can never drive an out-of-bounds deletion
// (FR-030; golang-security path-traversal guidance).
func (a *App) safeTargetForRemoval(p *project, scope, agentID, name, recorded string) (string, bool) {
	ag, found := a.agents.Get(agentID)
	if !found {
		return "", false
	}

	var boundary string
	if scope == string(installer.ScopeGlobal) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		boundary = ag.GlobalSkillDir(home)
	} else {
		boundary = ag.ProjectSkillDir(p.root)
	}
	expected := filepath.Join(boundary, name)

	// The recorded path must resolve to the same place the adapter derives, and
	// the derived path must stay within its boundary (a malicious skill name like
	// "../../x" is rejected here).
	if !samePath(resolveTarget(p.root, recorded), expected) || !withinDir(expected, boundary) {
		return "", false
	}
	return expected, true
}

// checkSafeTargetRemoval verifies — without deleting anything — that an
// agent's target may safely be removed: confined to its expected,
// adapter-derived path, and — for a real (copy-mode) directory — still
// matching gskill's recorded content. Content that no longer matches
// contentHash (the skill's recorded canonical content hash) is not
// gskill's to delete, so the removal fails closed instead. This check is
// unconditional — there is no Force parameter and none should be threaded
// in; --force only affects content-hash acceptance during installation,
// never this removal-ownership check (spec 013 FR-011/FR-013; research.md
// Decision 3).
//
// Whether the ownership check applies is decided from what is actually on
// disk (via os.Lstat), never from a recorded "mode" string: a missing or
// stale Installation.Modes entry must not silently fall through as
// "symlink, no check needed" while a real, foreign-editable directory sits
// at the target. A target that doesn't exist at all needs no check.
//
// Callers that must remove several targets for one skill as a unit (e.g.
// dropping multiple agents from a skill in one narrowing run) MUST check
// every target with this function before removing any of them — os.Lstat
// and active.Owned only read, so running every check first, then every
// removal, means a single foreign-modified target aborts the whole batch
// with zero partial deletions, instead of leaving some agents' files gone
// while others remain (spec 013 FR-011).
func (a *App) checkSafeTargetRemoval(p *project, scope, agentID, name, recorded, contentHash string) (target string, safe bool, err error) {
	target, ok := a.safeTargetForRemoval(p, scope, agentID, name, recorded)
	if !ok {
		a.log.Warn("skipping removal of out-of-bounds or mismatched target",
			"skill", name, "agent", agentID, "recorded", recorded)
		return "", false, nil
	}
	info, statErr := os.Lstat(target)
	switch {
	case statErr == nil && info.Mode()&os.ModeSymlink == 0:
		roots := []string{p.store.Root(), active.Dir(p.root)}
		if !active.Owned(target, roots, contentHash) {
			return "", false, errs.WithHint(
				fmt.Errorf("%w: %s target for skill %q is not gskill-managed content (modified since install)",
					errs.ErrInvalidLock, agentID, name),
				"the content differs from what gskill installed; resolve it manually before narrowing this skill's agents")
		}
	case statErr != nil && !os.IsNotExist(statErr):
		// A target that genuinely doesn't exist needs no check, but any other
		// stat failure (e.g. a permission error, or a symlink cycle) must not
		// silently fall through as "safe to remove without checking" — that
		// would skip the ownership check for a reason other than absence.
		return "", false, fmt.Errorf("stat %s target for skill %q: %w", agentID, name, statErr)
	}
	return target, true, nil
}

// samePath reports whether two paths refer to the same absolute, cleaned location.
func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aa == bb
}

// withinDir reports whether path is confined to dir (dir itself or beneath it),
// rejecting "../" escapes and absolute paths outside dir.
func withinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
