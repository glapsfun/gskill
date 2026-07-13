package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// listCmd lists installed skills with their drift status, active-layer
// health, and per-agent health — the merged view that used to require a
// separate `gskill status` command (spec 013).
type listCmd struct{}

// Help returns the detailed help shown by `gskill list --help`.
func (listCmd) Help() string {
	return examplesHelp(
		"gskill list",
		"gskill list --json",
	)
}

// Run executes `gskill list`.
func (listCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	skills, err := a.List(ctx, string(root))
	if err != nil {
		return err
	}
	human := renderListTable(skills)
	if out.Interactive() {
		human = renderListStyled(skills)
	}
	return out.Result(human, ListJSON(skills))
}

// ListJSON builds the stable --json object for a list result. `agents` keeps
// its existing shape (plain agent-ID strings); `active`, `agent_health`,
// `commit`, and `content_hash` are additive fields carrying what `gskill
// status --json` used to report on its own (spec 013 FR-005, clarification
// Q2).
func ListJSON(skills []app.ListedSkill) map[string]any {
	rows := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		agents := s.Agents
		if agents == nil {
			agents = []string{}
		}
		agentHealth := s.AgentHealth
		if agentHealth == nil {
			agentHealth = []app.AgentHealthEntry{}
		}
		rows = append(rows, map[string]any{
			"name":         s.Name,
			"source":       s.Source,
			"version":      s.Version,
			"status":       s.Status,
			"agents":       agents,
			"commit":       s.Commit,
			"content_hash": s.ContentHash,
			"active":       s.Active,
			"agent_health": agentHealth,
		})
	}
	return map[string]any{"skills": rows}
}

// noSkillsInstalled is the shared empty-state message for both the styled
// and plain `gskill list` renderers.
const noSkillsInstalled = "No skills installed."

// renderListTable renders a human-readable table. The four pre-existing
// columns keep their current NAME/STATUS/VERSION/SOURCE positional order —
// a divergence from the styled renderer's NAME/VERSION/SOURCE/STATUS order
// that predates this feature — with ACTIVE and AGENTS appended after it, so
// no script parsing this output by column position breaks (spec 013,
// contracts/list-command.md §2).
func renderListTable(skills []app.ListedSkill) string {
	if len(skills) == 0 {
		return noSkillsInstalled
	}
	var b strings.Builder
	for _, s := range skills {
		_, _ = fmt.Fprintf(&b, "%-24s %-10s %-14s %-24s %-10s %s\n",
			s.Name, s.Status, s.Version, s.Source, s.Active, agentHealthCellPlain(s.AgentHealth))
	}
	return strings.TrimRight(b.String(), "\n")
}

// agentHealthCellPlain renders one row's AGENTS cell as unstyled text: each
// agent as "id health", joined by ", ".
func agentHealthCellPlain(agents []app.AgentHealthEntry) string {
	cells := make([]string, 0, len(agents))
	for _, ag := range agents {
		cells = append(cells, ag.ID+" "+ag.Health)
	}
	return strings.Join(cells, ", ")
}
