package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/tui"
)

// onboardCmd starts the guided onboarding wizard without a predefined source
// (spec 011 FR-002): the flow first asks where skills should come from —
// configured repositories or a typed source — then continues like `add`.
type onboardCmd struct{}

// Help returns the detailed help shown by `gskill onboard --help`.
func (onboardCmd) Help() string {
	return examplesHelp(
		"gskill onboard",
		"gskill add github.com/owner/repo   # onboarding with a known source",
	)
}

// Run executes `gskill onboard`. It is interactive-only: in a non-interactive
// session it exits with a usage error and points at the scriptable `add`.
// Deliberately no out.withFetchProgress here: the wizard's bubbletea program
// owns the terminal, and the raw stderr renderer would corrupt its screen.
func (c onboardCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	if !out.Interactive() || out.JSON() || !stdinIsTTY() {
		return errs.WithHint(
			fmt.Errorf("%w: onboarding is interactive-only", errs.ErrUsage),
			"use 'gskill add <source>' with --skill/--agent/--yes for scripted installs")
	}

	var (
		src  string
		disc app.DiscoverResult
	)
	phases := tui.WizardPhases{
		ValidateSource: func(value string) error {
			_, err := source.Parse(value)
			return err
		},
		Discover: func(ctx context.Context) (app.DiscoverResult, error) {
			d, err := a.DiscoverSource(ctx, app.DiscoverRequest{Root: string(root), Source: src})
			if err != nil {
				return app.DiscoverResult{}, err
			}
			disc = d
			return d, nil
		},
		Plan: func(ctx context.Context, s *tui.Session) (app.InstallPlan, error) {
			return a.PlanInstall(ctx, app.PlanRequest{
				Root: string(root), Source: src,
				Version: s.Version, Ref: s.RefSpec, Commit: s.Commit,
				Discover: disc, Selected: s.Selected, AgentIDs: s.AgentIDs,
			})
		},
		Execute: func(ctx context.Context, plan app.InstallPlan, progress func(app.ProgressEvent)) (app.AddResult, error) {
			return a.ExecutePlan(ctx, plan, progress)
		},
		Agents: func(ctx context.Context) ([]app.AgentChoice, error) {
			return a.AgentChoices(ctx, string(root))
		},
		Versions: func(ctx context.Context) (app.VersionList, error) {
			return a.ListVersions(ctx, string(root), src, g.Offline)
		},
	}
	// The wizard learns the source on its first step; the phase closures read
	// it through this hook when discovery starts.
	phases.SourceChosen = func(value string) { src = value }

	outcome, err := runWizardFn(ctx, tui.WizardConfig{
		Session:           tui.Session{ApprovalAnswered: g.Yes},
		Phases:            phases,
		SourceSuggestions: a.Config().Repositories,
	}, true)
	if err != nil {
		return err
	}
	return finishWizardOutcome(out, outcome)
}
