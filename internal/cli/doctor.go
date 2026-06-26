package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// doctorCmd checks the environment and declared requirements.
type doctorCmd struct{}

// Run executes `gskill doctor`.
func (doctorCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	report, err := a.Doctor(ctx, string(root))
	if err != nil {
		return err
	}

	for _, w := range report.Warnings {
		out.Diag("warning: %s", w)
	}

	reqs := make([]map[string]any, 0, len(report.Requirements))
	for _, r := range report.Requirements {
		reqs = append(reqs, map[string]any{
			"skill": r.Skill, "kind": r.Kind, "name": r.Name,
			"satisfied": r.Satisfied, "checked": r.Checked,
		})
	}

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "git available:   %v\n", report.GitAvailable)
	_, _ = fmt.Fprintf(&b, "detected agents: %s\n", strings.Join(report.DetectedAgents, ", "))
	_, _ = fmt.Fprintf(&b, "warnings:        %d", len(report.Warnings))

	return out.Result(b.String(), map[string]any{
		"git_available":   report.GitAvailable,
		"detected_agents": report.DetectedAgents,
		"requirements":    reqs,
		"warnings":        report.Warnings,
	})
}
