package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// outdatedCmd reports available updates.
type outdatedCmd struct {
	ExitCode bool `name:"exit-code" help:"Exit 8 if any update is available."`
}

// Help returns the detailed help shown by `gskill outdated --help`.
func (outdatedCmd) Help() string {
	return examplesHelp(
		"gskill outdated",
		"gskill outdated --exit-code",
	)
}

// Run executes `gskill outdated`.
func (c outdatedCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	report, err := a.Outdated(ctx, string(root))
	if err != nil {
		return err
	}

	skills := make([]map[string]any, 0, len(report.Skills))
	available := 0
	for _, s := range report.Skills {
		if s.Available {
			available++
			out.Diag("%s: %s -> %s", s.Name, s.Current, s.Latest)
		}
		skills = append(skills, map[string]any{
			"name": s.Name, "current": s.Current, "latest": s.Latest, "available": s.Available,
		})
	}

	human := fmt.Sprintf("%d skill(s) up to date", len(report.Skills))
	if report.AnyAvailable {
		human = fmt.Sprintf("%d update(s) available", available)
	}
	if rErr := out.Result(human, map[string]any{"any_available": report.AnyAvailable, "skills": skills}); rErr != nil {
		return rErr
	}
	if c.ExitCode && report.AnyAvailable {
		return errs.WithHint(errs.ErrUpdateAvailable, "run 'gskill update' to advance skills within their constraints")
	}
	return nil
}
