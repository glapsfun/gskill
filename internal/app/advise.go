package app

import (
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// recordAdvice is the non-fatal, agent-specific advice for a skill as its lock
// entry now stands: content problems from each targeted agent.Advisor (spec
// 027 FR-011) and, in project scope, each targeted agent.ProjectNoter's note
// (FR-012). It reads the entry rather than an installer result, so agents the
// run did not touch, and skills that were already up to date, still count.
func (a *App) recordAdvice(p *project, name string, rec skillslock.Record) []string {
	out := a.lockedAdvice(p, name, rec)
	if rec.Installation.Scope != string(installer.ScopeGlobal) {
		out = append(out, a.projectNotes(rec.Installation.Agents)...)
	}
	return out
}

// projectNotes returns the note of each agent.ProjectNoter among ids.
func (a *App) projectNotes(ids []string) []string {
	var out []string
	for _, id := range ids {
		if ag, ok := a.agents.Get(id); ok {
			if n, ok := ag.(agent.ProjectNoter); ok {
				out = append(out, n.ProjectNote())
			}
		}
	}
	return out
}

// lockedAdvice is the content-problem part of recordAdvice, which `project
// check` reports on its own (it describes content, not the environment).
func (a *App) lockedAdvice(p *project, name string, locked skillslock.Record) []string {
	global := locked.Installation.Scope == string(installer.ScopeGlobal)
	dirs := make(map[string]string, len(locked.Installation.Agents))
	for _, id := range locked.Installation.Agents {
		if dir := a.agentTargetDir(p, id, name, locked.Installation.Targets[id], global); dir != "" {
			dirs[id] = dir
		}
	}
	return a.advise(name, dirs)
}

// advise asks each agent.Advisor among the agents in dirs (agent ID to that
// agent's installed skill directory) about the skill installed as name.
func (a *App) advise(name string, dirs map[string]string) []string {
	var out []string
	for _, id := range sortedKeys(dirs) {
		ag, ok := a.agents.Get(id)
		if !ok {
			continue
		}
		if adv, ok := ag.(agent.Advisor); ok {
			out = append(out, adv.Advise(name, dirs[id])...)
		}
	}
	return out
}

// dedupeLines drops repeated lines, keeping first-seen order, so advice that
// applies to many skills is shown once per command.
func dedupeLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	seen := make(map[string]bool, len(lines))
	out := lines[:0:0]
	for _, l := range lines {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}
