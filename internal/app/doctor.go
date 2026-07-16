package app

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// RequirementCheck is one declared requirement and whether the environment
// satisfies it. Requirements are recorded and surfaced only — gskill never
// resolves them transitively or auto-installs anything (FR-032).
type RequirementCheck struct {
	Skill     string
	Kind      string // command | environment | skill | mcp
	Name      string
	Satisfied bool
	Checked   bool // false for kinds gskill cannot verify (e.g. mcp)
}

// DoctorReport is the result of `gskill doctor`.
type DoctorReport struct {
	GitAvailable   bool
	DetectedAgents []string
	Requirements   []RequirementCheck
	Warnings       []string
}

// Doctor checks the environment (git, detected agents) and reports declared
// requirements, warning on any that are unmet (FR-032). It never installs.
func (a *App) Doctor(ctx context.Context, root string) (DoctorReport, error) {
	report := DoctorReport{}

	_, gitErr := exec.LookPath("git")
	report.GitAvailable = gitErr == nil
	if !report.GitAvailable {
		report.Warnings = append(report.Warnings, "git not found on PATH; git sources will be unavailable")
	}

	detected, err := a.agents.Detect(ctx, root)
	if err != nil {
		return DoctorReport{}, err
	}
	for _, ag := range detected {
		report.DetectedAgents = append(report.DetectedAgents, ag.ID())
	}

	if hasPopulatedProjectStore(root) && a.cfg.StoreScope != config.StoreScopeProject {
		report.Warnings = append(report.Warnings,
			"this project uses the legacy project-local store; run 'gskill migrate global-store' to share content across projects")
	}

	lf, err := loadOrNewLock(openProject(root).lockPath)
	if err != nil {
		return DoctorReport{}, err
	}
	for _, name := range sortedKeys(lf.Skills) {
		checkRequirements(name, lf.Skills[name].Requires, lf, &report)
	}
	return report, nil
}

// checkRequirements verifies a skill's declared requirements and appends results
// and warnings to the report.
func checkRequirements(skill string, req skillslock.Requires, lf *skillslock.State, report *DoctorReport) {
	for _, cmd := range req.Commands {
		_, err := exec.LookPath(cmd)
		report.add(skill, "command", cmd, err == nil, true)
	}
	for _, env := range req.Environment {
		_, ok := os.LookupEnv(env)
		report.add(skill, "environment", env, ok, true)
	}
	for _, dep := range req.Skills {
		name := requirementName(dep)
		_, installed := lf.Skills[name]
		report.add(skill, "skill", dep, installed, true)
	}
	for _, mcp := range req.MCP {
		// MCP servers cannot be verified by gskill; surface only.
		report.add(skill, "mcp", mcp, false, false)
	}
}

// add records a requirement check and warns when an unmet, checkable
// requirement is found.
func (r *DoctorReport) add(skill, kind, name string, satisfied, checked bool) {
	r.Requirements = append(r.Requirements, RequirementCheck{
		Skill: skill, Kind: kind, Name: name, Satisfied: satisfied, Checked: checked,
	})
	if checked && !satisfied {
		r.Warnings = append(r.Warnings, skill+": unmet "+kind+" requirement: "+name)
	}
}

// requirementName extracts the bare name from a "name [constraint]" requirement.
func requirementName(req string) string {
	if i := strings.IndexAny(req, " \t><=^~"); i >= 0 {
		return strings.TrimSpace(req[:i])
	}
	return req
}
