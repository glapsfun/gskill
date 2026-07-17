package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
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
	p, pErr := a.openProjectScoped(root)
	if pErr != nil {
		return VerifyReport{}, pErr
	}
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
func verifySkill(root, name string, locked skillslock.Record) SkillVerify {
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

// Check produces a drift report over the three-hop chain (store → active →
// agent). With failOnDrift, any drift returns exit 7 (FR-016). A present-but-
// corrupt store fails closed with exit 6 regardless of the flag (FR-018).
func (a *App) Check(_ context.Context, root string, failOnDrift bool) (CheckReport, error) {
	p, pErr := a.openProjectScoped(root)
	if pErr != nil {
		return CheckReport{}, pErr
	}
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return CheckReport{}, err
	}

	health, err := a.healthByName(p, lf, true)
	if err != nil {
		return CheckReport{}, err
	}

	var report CheckReport
	integrityFault := false
	for _, name := range sortedKeys(lf.Skills) {
		status := chainStatus(p.root, name, lf, health[name])
		report.Skills = append(report.Skills, SkillCheck{Name: name, Status: string(status)})
		if status != integrity.DriftInstalled {
			report.HasDrift = true
		}
		integrityFault = integrityFault || health[name].IntegrityFault()
	}
	if integrityFault {
		return report, errs.ErrIntegrity
	}
	if failOnDrift && report.HasDrift {
		return report, errs.ErrDrift
	}
	return report, nil
}

// chainStatus classifies a skill from lock metadata, then downgrades a
// metadata-clean skill to drift when its three-hop chain is broken (e.g. a
// dangling active link the lstat presence check cannot see).
func chainStatus(root, name string, lf *skillslock.State, h SkillHealth) integrity.DriftStatus {
	status := classifySkill(root, name, lf)
	if status == integrity.DriftInstalled && h.Name != "" && !h.Healthy() {
		return integrity.DriftModified
	}
	return status
}

// healthByName evaluates the chain health of every locked skill, keyed by
// name. verifyHash controls whether store content is re-hashed: Check passes
// true so it fails closed (exit 6) on a tampered store or a corrupt copy
// target, not just on structural drift; List passes false for the same
// cheap, non-verifying path `status` always used.
func (a *App) healthByName(p *project, lf *skillslock.State, verifyHash bool) (map[string]SkillHealth, error) {
	healths, err := a.evaluateHealth(p, lf, verifyHash)
	if err != nil {
		return nil, err
	}
	out := make(map[string]SkillHealth, len(healths))
	for _, h := range healths {
		out[h.Name] = h
	}
	return out, nil
}

// classifySkill builds a SkillState from lock/fs and classifies it.
func classifySkill(root, name string, lf *skillslock.State) integrity.DriftStatus {
	locked, inLock := lf.Skills[name]

	state := integrity.SkillState{InLock: inLock, SourceAvailable: true}
	if inLock {
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
