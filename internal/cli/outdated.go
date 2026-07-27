package cli

import (
	"context"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// outdatedCmd reports available updates. It renders the same shared update
// plan as `gskill update --list` (spec 018 FR-008), so the two commands can
// never disagree about eligibility.
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
func (c outdatedCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	plan, err := a.PlanUpdate(ctx, app.UpdatePlanRequest{
		Root: string(root), Offline: g.Offline, NoCache: g.NoCache,
	})
	done()
	if err != nil {
		return err
	}

	if rErr := out.Result(renderUpdateList(out, plan, false), OutdatedJSON(plan)); rErr != nil {
		return rErr
	}
	if c.ExitCode && plan.AnyAvailable() {
		return errs.WithHint(errs.ErrUpdateAvailable, "run 'gskill update' to advance skills within their constraints")
	}
	// A run that could not check its remotes must not exit like a verified
	// one — CI gates would otherwise pass green during an outage.
	return discoveryFailureErr(plan)
}

// OutdatedJSON keeps the historical `outdated --json` fields (name, current,
// latest, available, any_available) and adds the plan's candidate/status
// detail additively (spec 018 FR-016).
func OutdatedJSON(plan app.UpdatePlan) map[string]any {
	skills := make([]map[string]any, 0, len(plan.Items))
	for _, it := range plan.Items {
		latest := it.Candidate
		if latest == "" {
			latest = it.Current
		}
		skills = append(skills, map[string]any{
			"name":      it.Name,
			"current":   it.Current,
			"latest":    latest,
			"available": it.Actionable(),
			"policy":    it.Policy,
			"status":    string(it.Status),
			"reason":    it.Reason,
		})
	}
	return map[string]any{
		"any_available": plan.AnyAvailable(),
		"skills":        skills,
		"not_checked":   countUnknown(plan),
	}
}
