package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// checkCmd reports fast drift status.
type checkCmd struct {
	FailOnDrift bool `name:"fail-on-drift" help:"Exit non-zero (7) if any drift is detected."`
}

// Help returns the detailed help shown by `gskill project check --help`.
func (checkCmd) Help() string {
	return examplesHelp(
		"gskill project check",
		"gskill project check --fail-on-drift",
	)
}

// Run executes `gskill project check` (alias: `gskill check`).
func (c checkCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	report, err := a.Check(ctx, string(root), c.FailOnDrift)
	skills := make([]map[string]any, 0, len(report.Skills))
	for _, s := range report.Skills {
		skills = append(skills, map[string]any{"name": s.Name, "status": s.Status})
	}

	human := out.summary(fmt.Sprintf("Checked %d skill(s): no drift", len(report.Skills)))
	if report.HasDrift {
		human = out.warnSummary(fmt.Sprintf("Drift detected in %d skill(s)", countDrift(report.Skills)))
	}
	if rErr := out.Result(human, map[string]any{"has_drift": report.HasDrift, "skills": skills}); rErr != nil {
		return rErr
	}
	if err != nil && errors.Is(err, errs.ErrDrift) {
		return errs.WithHint(err, "run 'gskill install' to reconcile disk with the lock")
	}
	return err
}

// countDrift counts skills whose status is not "installed".
func countDrift(skills []app.SkillCheck) int {
	n := 0
	for _, s := range skills {
		if s.Status != "installed" {
			n++
		}
	}
	return n
}
