package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// statusCmd reports installed skills, their agents, modes, and per-target health.
type statusCmd struct{}

// Help returns the detailed help shown by `gskill status --help`.
func (statusCmd) Help() string {
	return examplesHelp(
		"gskill status",
		"gskill status --json",
	)
}

// Run executes `gskill status`.
func (statusCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	report, err := a.Status(ctx, string(root))
	if err != nil {
		return err
	}

	lines := make([]string, 0, len(report.Skills))
	for _, s := range report.Skills {
		parts := make([]string, 0, len(s.Agents))
		for _, ag := range s.Agents {
			parts = append(parts, fmt.Sprintf("%s=%s", ag.ID, ag.Health))
		}
		lines = append(lines, fmt.Sprintf("%-24s active=%-8s %s", s.Name, s.Active, strings.Join(parts, " ")))
	}
	human := fmt.Sprintf("%d skill(s)", len(report.Skills))
	if len(lines) > 0 {
		human = strings.Join(lines, "\n")
	}
	if out.Interactive() {
		human = renderStatusStyled(report)
	}
	return out.Result(human, map[string]any{"skills": report.Skills})
}
