package app

import (
	"context"

	"github.com/glapsfun/gskill/internal/manifest"
)

// AgentStatus reports one agent target's mode and health for a skill.
type AgentStatus struct {
	ID     string `json:"id"`
	Mode   string `json:"mode"`
	Health string `json:"health"`
}

// SkillStatus reports one skill's source, resolved identity, and per-agent
// health across the active layer and agent targets.
type SkillStatus struct {
	Name        string        `json:"name"`
	Source      string        `json:"source"`
	Commit      string        `json:"commit"`
	ContentHash string        `json:"content_hash"`
	Active      string        `json:"active"`
	Agents      []AgentStatus `json:"agents"`
}

// StatusReport aggregates a status run.
type StatusReport struct {
	Skills []SkillStatus
}

// Status reports the installed skills, their target agents, install modes, and
// per-target health (FR-021). It is read-only and always succeeds on a readable
// project (exit 0), so it composes in scripts; use `check` for a non-zero exit on
// drift. An unreadable manifest is a hard error (exit 3).
func (a *App) Status(_ context.Context, root string) (StatusReport, error) {
	p := openProject(root)
	if p.manifestExists() {
		if _, err := manifest.Load(p.manifestPath); err != nil {
			return StatusReport{}, err
		}
	}
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return StatusReport{}, err
	}

	health, err := a.evaluateHealth(p, lf, false)
	if err != nil {
		return StatusReport{}, err
	}

	var report StatusReport
	for _, h := range health {
		locked := lf.Skills[h.Name]
		ss := SkillStatus{
			Name:        h.Name,
			Source:      locked.Source.Original,
			Commit:      locked.Resolved.Commit,
			ContentHash: locked.Resolved.ContentHash,
			Active:      string(h.ActiveState),
		}
		for _, id := range sortedKeys(h.Agents) {
			ss.Agents = append(ss.Agents, AgentStatus{
				ID:     id,
				Mode:   h.Modes[id],
				Health: string(h.Agents[id]),
			})
		}
		report.Skills = append(report.Skills, ss)
	}
	return report, nil
}
