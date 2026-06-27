package app

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
)

// SkillVerify is one skill's integrity-verification outcome.
type SkillVerify struct {
	Name     string
	OK       bool
	Expected string
	Actual   string
	Issue    string // ok | missing | mismatch
}

// VerifyReport aggregates a verify run.
type VerifyReport struct {
	Skills []SkillVerify
	OK     bool
}

// Verify re-hashes every installed skill against the lockfile, failing closed on
// the first mismatch (exit 6, FR-015).
func (a *App) Verify(_ context.Context, root string) (VerifyReport, error) {
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return VerifyReport{}, err
	}

	report := VerifyReport{OK: true}
	for _, name := range sortedKeys(lf.Skills) {
		report.Skills = append(report.Skills, verifySkill(p.root, name, lf.Skills[name]))
	}
	for _, sv := range report.Skills {
		if !sv.OK {
			report.OK = false
		}
	}
	if !report.OK {
		return report, errs.ErrIntegrity
	}
	return report, nil
}

// verifySkill checks every agent target of one locked skill.
func verifySkill(root, name string, locked lockfile.LockedSkill) SkillVerify {
	expected := locked.Resolved.ContentHash
	sv := SkillVerify{Name: name, OK: true, Expected: expected, Issue: "ok"}

	for _, agentID := range sortedKeys(locked.Installation.Targets) {
		dir := resolveTarget(root, locked.Installation.Targets[agentID])
		ok, actual, err := integrity.VerifyDir(dir, expected)
		if err != nil {
			sv.OK, sv.Issue = false, "missing"
			return sv
		}
		if !ok {
			sv.OK, sv.Issue, sv.Actual = false, "mismatch", actual
			return sv
		}
	}
	return sv
}

// SkillCheck is one skill's fast drift status.
type SkillCheck struct {
	Name   string
	Status string
}

// CheckReport aggregates a check run.
type CheckReport struct {
	Skills   []SkillCheck
	HasDrift bool
}

// Check produces a fast, metadata-only drift report. With failOnDrift, any drift
// returns exit 7 (FR-016, FR-017).
func (a *App) Check(_ context.Context, root string, failOnDrift bool) (CheckReport, error) {
	p := openProject(root)
	m := manifest.New()
	if p.manifestExists() {
		loaded, err := manifest.Load(p.manifestPath)
		if err != nil {
			return CheckReport{}, err
		}
		m = loaded
	}
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return CheckReport{}, err
	}

	var report CheckReport
	for _, name := range unionKeys(m.Skills, lf.Skills) {
		status := classifySkill(p.root, name, m, lf)
		report.Skills = append(report.Skills, SkillCheck{Name: name, Status: string(status)})
		if status != integrity.DriftInstalled {
			report.HasDrift = true
		}
	}
	if failOnDrift && report.HasDrift {
		return report, errs.ErrDrift
	}
	return report, nil
}

// classifySkill builds a SkillState from manifest/lock/fs and classifies it.
func classifySkill(root, name string, m *manifest.Manifest, lf *lockfile.Lockfile) integrity.DriftStatus {
	ms, inManifest := m.Skills[name]
	locked, inLock := lf.Skills[name]

	state := integrity.SkillState{InManifest: inManifest, InLock: inLock, SourceAvailable: true}
	if inLock {
		state.SourceChanged = inManifest && locked.Source.Original != ms.Source
		state.TargetsTotal = len(locked.Installation.Targets)
		for _, target := range locked.Installation.Targets {
			if _, err := os.Lstat(resolveTarget(root, target)); err == nil {
				state.TargetsPresent++
			}
		}
	}
	return integrity.Classify(state)
}

// resolveTarget resolves a recorded target path against the project root.
func resolveTarget(root, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(root, target)
}

// unionKeys returns the sorted union of two maps' keys.
func unionKeys[A, B any](a map[string]A, b map[string]B) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
