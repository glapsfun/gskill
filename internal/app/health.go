package app

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/overrides"
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
	// TargetSymlinklessCheckout is the core.symlinks=false artifact: a plain
	// file holding the committed link's text (spec 022 FR-016). Detected and
	// reported only — never repaired; symlinks are a platform requirement.
	TargetSymlinklessCheckout TargetState = "symlinkless-checkout"
)

// SkillHealth is the evaluated three-hop state for one locked skill.
type SkillHealth struct {
	Name        string
	Scope       string
	ActiveState active.Health
	ActivePath  string // project-relative active entry
	Agents      map[string]TargetState
	Modes       map[string]string
	Targets     map[string]string // agentID -> recorded target path (repo-relative)
	// OverrideDrift names the override input whose content no longer matches
	// the identity recorded in the lock (spec 023 FR-010). It is reported
	// separately from content drift because the causes and the remedies
	// differ: here the *declaration* moved, not the installed content.
	OverrideDrift string
}

// Healthy reports whether every rung of the chain is in a good state (spec
// 022: committed content → agent links; there is no store rung).
func (h SkillHealth) Healthy() bool {
	if h.OverrideDrift != "" {
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
	if h.OverrideDrift != "" {
		out = append(out, fmt.Sprintf(
			"%s: override input %s changed since install; the committed content no longer matches its declaration",
			h.Name, h.OverrideDrift))
	}
	if h.Scope != string(installer.ScopeGlobal) && h.ActiveState != active.HealthOK {
		if h.ActiveState == active.HealthDrifted {
			out = append(out, fmt.Sprintf("%s: committed content at %s no longer matches skills-lock.json (drifted)", h.Name, h.ActivePath))
		} else {
			out = append(out, fmt.Sprintf("%s: active entry %s", h.Name, h.ActiveState))
		}
	}
	for _, id := range sortedKeys(h.Agents) {
		st := h.Agents[id]
		if st == TargetOKSymlink || st == TargetOKCopy {
			continue
		}
		if st == TargetSymlinklessCheckout {
			out = append(out, symlinklessCheckoutMsg(cmp.Or(h.Targets[id], h.Name)))
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s %s", id, h.Name, st))
	}
	return out
}

// symlinklessCheckoutMsg is the single error line check/doctor emit for a
// degraded checkout — wording frozen by spec 022 (contracts/cli-surface.md).
func symlinklessCheckoutMsg(relPath string) string {
	return fmt.Sprintf("agent link %s is a plain file, not a symlink (checkout made without symlink support, e.g. core.symlinks=false); re-clone on a symlink-capable filesystem — gskill does not repair degraded checkouts", relPath)
}

// isSymlinklessArtifact reports whether path is a regular file holding link
// text into the active layer — what a core.symlinks=false checkout leaves in
// place of a committed agent link.
func isSymlinklessArtifact(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return false
	}
	data, err := os.ReadFile(path) //nolint:gosec // recorded agent-target path, size-capped above
	if err != nil {
		return false
	}
	target := strings.TrimSpace(string(data))
	return !strings.Contains(target, "\n") && strings.Contains(filepath.ToSlash(target), ".agents/skills/")
}

// IntegrityFault reports whether any fault is a content-integrity failure — a
// corrupt copy target — which maps to a fail-closed exit code. Drifted
// committed content is deliberately NOT one: spec 022 reports it as drift
// (exit 7); `gskill verify` is the fail-closed hash check (exit 6).
func (h SkillHealth) IntegrityFault() bool {
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
	out := make([]SkillHealth, 0, len(lf.Skills))
	for _, name := range sortedKeys(lf.Skills) {
		h, evalErr := a.evaluateSkill(p, name, lf.Skills[name], verifyHash)
		if evalErr != nil {
			return nil, evalErr
		}
		out = append(out, h)
	}
	return out, nil
}

// evaluateSkill computes the health of a single locked skill.
func (a *App) evaluateSkill(p *project, name string, locked skillslock.Record, verifyHash bool) (SkillHealth, error) {
	hash := locked.Resolved.ContentHash
	// Spec 022: there is no store rung — the committed repo copy is the
	// content, evaluated below as the active entry's health.
	h := SkillHealth{
		Name:       name,
		Scope:      locked.Installation.Scope,
		ActivePath: activePathOf(locked, name),
		Agents:     make(map[string]TargetState, len(locked.Installation.Agents)),
		Modes:      locked.Installation.Modes,
		Targets:    locked.Installation.Targets,
	}

	h.OverrideDrift = a.overrideDriftOf(p, name, locked)

	global := locked.Installation.Scope == string(installer.ScopeGlobal)
	legacyRoots := p.legacyStoreRoots()
	linkTarget := active.Path(p.root, name)
	if !global {
		state, err := active.HealthOf(p.root, name, hash, legacyRoots...)
		if err != nil {
			return SkillHealth{}, err
		}
		h.ActiveState = state
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
		state, err := agentTargetState(targetDir, linkTarget, legacyRoots, recordedMode, verifyHash, hash)
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
func agentTargetState(targetDir, linkTarget string, legacyRoots []string, recordedMode string, verifyHash bool, expectedHash string) (TargetState, error) {
	info, err := os.Lstat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return TargetMissing, nil
		}
		return "", fmt.Errorf("stat target %s: %w", targetDir, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		if info.Mode().IsRegular() {
			// A plain file where a link should be: the core.symlinks=false
			// artifact (detected, never repaired) — or foreign content.
			if isSymlinklessArtifact(targetDir) {
				return TargetSymlinklessCheckout, nil
			}
			return TargetForeign, nil
		}
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
	for _, legacyRoot := range legacyRoots {
		if under(resolved, legacyRoot) {
			return TargetLegacyStore, nil // links straight into a pre-022 store
		}
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

// overrideDriftOf names the override input responsible when the declaration's
// current identity no longer matches what the lock recorded (spec 023 FR-010).
//
// It compares digests rather than watching files: the digest covers the
// declaration *and* the bytes of every file it references, so an edit anywhere
// in that set changes it. The first input whose content hash differs from the
// recorded one is named, because "something changed" is not actionable and
// "house-rules.md changed" is.
func (a *App) overrideDriftOf(p *project, name string, locked skillslock.Record) string {
	spec, digest, err := a.overrideFor(p.root, name)
	if err != nil || digest == locked.Resolved.OverrideDigest {
		return ""
	}
	if digest == "" && locked.Resolved.OverrideDigest == "" {
		return ""
	}
	// Name the specific input when one can be identified; fall back to the
	// declaration itself when the change is structural (a kind added, an entry
	// reordered) rather than a file edit.
	for _, rel := range spec.Inputs() {
		if declaredBefore(rel, locked.Resolved.Override) {
			return rel
		}
	}
	return manifest.FileName
}

// declaredBefore reports whether rel was already part of the declaration the
// lock recorded. An input present in both is one whose *bytes* the digest
// disagreed over, which is the case a per-file comparison can attribute to a
// specific file.
func declaredBefore(rel string, recorded *skillslock.OverrideDecl) bool {
	if recorded == nil {
		return true
	}
	for _, known := range (&overrides.Spec{
		Replace: recorded.Replace,
		Patch:   recorded.Patch,
		Prepend: recorded.Prepend,
		Append:  recorded.Append,
	}).Inputs() {
		if known == rel {
			// Present in both: its bytes are what the digest disagreed over.
			return true
		}
	}
	return false
}
