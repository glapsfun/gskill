package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/integrity"
)

// TargetState classifies one agent target's health relative to the locked state.
type TargetState string

// Agent-target health states.
const (
	TargetOKSymlink    TargetState = "ok-symlink"    // symlink into the active entry
	TargetOKCopy       TargetState = "ok-copy"       // a copy whose content is present
	TargetMissing      TargetState = "missing"       // no target on disk
	TargetBroken       TargetState = "broken-link"   // symlink whose target is gone
	TargetForeign      TargetState = "foreign"       // present but not gskill-managed
	TargetModeMismatch TargetState = "mode-mismatch" // recorded mode differs from on disk
	TargetLegacyStore  TargetState = "legacy-store"  // symlink directly into the store (pre-active-layer)
	TargetCorrupt      TargetState = "corrupt"       // a copy whose content no longer matches the lock
)

// SkillHealth is the evaluated three-hop state for one locked skill.
type SkillHealth struct {
	Name         string
	Scope        string
	StorePresent bool
	Hashed       bool // whether store/copy content was hash-verified this evaluation
	StoreHashOK  bool // only meaningful when Hashed
	StorePath    string
	ActiveState  active.Health
	ActivePath   string // project-relative active entry
	Agents       map[string]TargetState
	Modes        map[string]string
}

// Healthy reports whether every rung of the chain is in a good state. When the
// store was hash-verified, a content mismatch is unhealthy (fail closed).
func (h SkillHealth) Healthy() bool {
	if !h.StorePresent {
		return false
	}
	if h.Hashed && !h.StoreHashOK {
		return false
	}
	if h.Scope != string(installer.ScopeGlobal) && h.ActiveState != active.HealthOK {
		return false
	}
	for _, st := range h.Agents {
		if st != TargetOKSymlink && st != TargetOKCopy {
			return false
		}
	}
	return true
}

// Faults returns human-readable descriptions of every non-OK rung.
func (h SkillHealth) Faults() []string {
	var out []string
	if !h.StorePresent {
		out = append(out, fmt.Sprintf("%s: store content %s missing", h.Name, h.StorePath))
	} else if h.Hashed && !h.StoreHashOK {
		out = append(out, h.Name+": store content hash mismatch")
	}
	if h.Scope != string(installer.ScopeGlobal) && h.ActiveState != active.HealthOK {
		out = append(out, fmt.Sprintf("%s: active entry %s", h.Name, h.ActiveState))
	}
	for _, id := range sortedKeys(h.Agents) {
		if st := h.Agents[id]; st != TargetOKSymlink && st != TargetOKCopy {
			out = append(out, fmt.Sprintf("%s:%s %s", id, h.Name, st))
		}
	}
	return out
}

// IntegrityFault reports whether any fault is a content-integrity failure — a
// hash-verified store mismatch or a corrupt copy target — which maps to a
// fail-closed exit code.
func (h SkillHealth) IntegrityFault() bool {
	if h.Hashed && h.StorePresent && !h.StoreHashOK {
		return true
	}
	for _, st := range h.Agents {
		if st == TargetCorrupt {
			return true
		}
	}
	return false
}

// evaluateHealth computes the three-hop health for every locked skill, sorted by
// name. When verifyHash is set, store content is re-hashed against the lockfile
// (the integrity check); otherwise only presence is checked (the cheap path used
// by reconcile to decide what to skip).
func (a *App) evaluateHealth(p *project, lf *skillslock.State, verifyHash bool) ([]SkillHealth, error) {
	storeRoot, err := filepath.Abs(p.contentRoot())
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}

	out := make([]SkillHealth, 0, len(lf.Skills))
	for _, name := range sortedKeys(lf.Skills) {
		h, evalErr := a.evaluateSkill(p, name, lf.Skills[name], storeRoot, verifyHash)
		if evalErr != nil {
			return nil, evalErr
		}
		out = append(out, h)
	}
	return out, nil
}

// evaluateSkill computes the health of a single locked skill.
func (a *App) evaluateSkill(p *project, name string, locked skillslock.Record, storeRoot string, verifyHash bool) (SkillHealth, error) {
	hash := locked.Resolved.ContentHash
	storePath := p.contentPath(hash)
	h := SkillHealth{
		Name:        name,
		Scope:       locked.Installation.Scope,
		StorePath:   storePath,
		ActivePath:  activePathOf(locked, name),
		StoreHashOK: true,
		Agents:      make(map[string]TargetState, len(locked.Installation.Agents)),
		Modes:       locked.Installation.Modes,
	}

	h.StorePresent = p.contentHas(hash)
	if h.StorePresent && verifyHash {
		hashes, err := integrity.HashDir(storePath)
		if err != nil {
			return SkillHealth{}, fmt.Errorf("hash store %s: %w", name, err)
		}
		h.Hashed = true
		h.StoreHashOK = hashes.ContentHash == hash
	}

	global := locked.Installation.Scope == string(installer.ScopeGlobal)
	linkTarget := storePath
	if !global {
		state, err := active.HealthOf(p.root, name, storePath)
		if err != nil {
			return SkillHealth{}, err
		}
		h.ActiveState = state
		linkTarget = active.Path(p.root, name)
	} else {
		h.ActiveState = active.HealthOK
	}

	for _, id := range locked.Installation.Agents {
		targetDir := a.agentTargetDir(p, id, name, locked.Installation.Targets[id], global)
		if targetDir == "" {
			h.Agents[id] = TargetForeign // unknown agent; cannot evaluate
			continue
		}
		recordedMode := locked.Installation.Modes[id]
		state, err := agentTargetState(targetDir, linkTarget, storeRoot, recordedMode, verifyHash, hash)
		if err != nil {
			return SkillHealth{}, err
		}
		h.Agents[id] = state
	}
	return h, nil
}

// agentTargetDir resolves the absolute agent target directory, preferring the
// lockfile-recorded path and falling back to the adapter's project skill dir.
func (a *App) agentTargetDir(p *project, id, name, recorded string, global bool) string {
	if recorded != "" {
		if global || filepath.IsAbs(recorded) {
			return recorded
		}
		return filepath.Join(p.root, recorded)
	}
	ag, ok := a.agents.Get(id)
	if !ok {
		return ""
	}
	if global {
		home, _ := os.UserHomeDir()
		return filepath.Join(ag.GlobalSkillDir(home), name)
	}
	return filepath.Join(ag.ProjectSkillDir(p.root), name)
}

// activePathOf returns the recorded active path, or the conventional one when the
// lock entry predates the active layer (legacy migration target).
func activePathOf(locked skillslock.Record, name string) string {
	if locked.Installation.ActivePath != "" {
		return locked.Installation.ActivePath
	}
	return active.Rel(name)
}

// agentTargetState classifies a single agent target on disk. When verifyHash is
// set, a copied target's content is hashed against expectedHash so a tampered or
// truncated copy is reported as corrupt rather than blindly accepted.
func agentTargetState(targetDir, linkTarget, storeRoot, recordedMode string, verifyHash bool, expectedHash string) (TargetState, error) {
	info, err := os.Lstat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return TargetMissing, nil
		}
		return "", fmt.Errorf("stat target %s: %w", targetDir, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		// A real directory: a copy (or a foreign dir).
		return copyTargetState(targetDir, recordedMode, verifyHash, expectedHash)
	}

	// A symlink: where does it resolve?
	resolved, err := readLinkAbs(targetDir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(resolved); err != nil {
		if os.IsNotExist(err) {
			return TargetBroken, nil
		}
		return "", fmt.Errorf("stat target link %s: %w", targetDir, err)
	}
	if recordedMode == string(installer.ModeCopy) {
		return TargetModeMismatch, nil // expected a copy, found a symlink
	}
	if pathEqual(resolved, linkTarget) {
		return TargetOKSymlink, nil
	}
	if under(resolved, storeRoot) {
		return TargetLegacyStore, nil // links straight into the store, pre-active-layer
	}
	return TargetModeMismatch, nil
}

// copyTargetState classifies a non-symlink (copy) agent target. With verifyHash
// its content is checked against expectedHash so a tampered copy is reported as
// corrupt rather than silently accepted.
func copyTargetState(targetDir, recordedMode string, verifyHash bool, expectedHash string) (TargetState, error) {
	if recordedMode == string(installer.ModeSymlink) {
		return TargetModeMismatch, nil
	}
	if verifyHash {
		ok, _, err := integrity.VerifyDir(targetDir, expectedHash)
		if err != nil {
			return "", fmt.Errorf("verify copy %s: %w", targetDir, err)
		}
		if !ok {
			return TargetCorrupt, nil
		}
	}
	return TargetOKCopy, nil
}

// readLinkAbs reads a symlink and returns an absolute, cleaned target.
func readLinkAbs(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", fmt.Errorf("read link %s: %w", path, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), nil
}

// pathEqual compares two paths after making them absolute and cleaned.
func pathEqual(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aa == bb
}

// under reports whether path is root or lives beneath it.
func under(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
