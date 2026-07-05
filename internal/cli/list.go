package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// listCmd lists installed skills with status.
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
	return out.Result(renderListTable(skills), ListJSON(skills))
}

// ListJSON builds the stable --json object for a list result.
func ListJSON(skills []app.ListedSkill) map[string]any {
	rows := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		agents := s.Agents
		if agents == nil {
			agents = []string{}
		}
		rows = append(rows, map[string]any{
			"name":    s.Name,
			"source":  s.Source,
			"version": s.Version,
			"status":  s.Status,
			"agents":  agents,
		})
	}
	return map[string]any{"skills": rows}
}

// renderListTable renders a human-readable table.
func renderListTable(skills []app.ListedSkill) string {
	if len(skills) == 0 {
		return "No skills installed."
	}
	var b strings.Builder
	for _, s := range skills {
		_, _ = fmt.Fprintf(&b, "%-24s %-10s %-14s %s\n", s.Name, s.Status, s.Version, s.Source)
	}
	return strings.TrimRight(b.String(), "\n")
}
