package app

import (
	"os"
	"path/filepath"
	"strings"

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

// removeSafeTarget deletes an agent target only when it is the confined,
// adapter-derived path; otherwise it skips and warns (never deletes an arbitrary
// path). It reports whether a deletion happened.
func (a *App) removeSafeTarget(p *project, scope, agentID, name, recorded string) (bool, error) {
	target, ok := a.safeTargetForRemoval(p, scope, agentID, name, recorded)
	if !ok {
		a.log.Warn("skipping removal of out-of-bounds or mismatched target",
			"skill", name, "agent", agentID, "recorded", recorded)
		return false, nil
	}
	if err := os.RemoveAll(target); err != nil {
		return false, err
	}
	return true, nil
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
