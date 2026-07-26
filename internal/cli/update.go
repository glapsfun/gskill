package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/tui"
)

// runUpdateSelectorFn indirects the interactive selector so tests can stub
// the TUI (the runWizardFn pattern).
var runUpdateSelectorFn = tui.SelectUpdates

// updateCmd advances skills to the newest version within their constraints.
type updateCmd struct {
	Skills []string `arg:"" optional:"" help:"Skills to update (default: all with available updates)."`
	List   bool     `help:"List available updates without applying them."`
	All    bool     `help:"With --list: also report up-to-date, pinned, and local skills."`
}

// Help returns the detailed help shown by `gskill update --help`.
func (updateCmd) Help() string {
	return examplesHelp(
		"gskill update",
		"gskill update --list",
		"gskill update --list --all",
		"gskill update my-skill",
	)
}

// Run executes `gskill update`.
func (c updateCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	if c.All && !c.List {
		return errs.New(errs.CodeUsage, "--all requires --list")
	}
	if c.List && len(c.Skills) > 0 {
		return errs.New(errs.CodeUsage, "--list reports the whole project and does not take skill names")
	}
	if c.List {
		return c.runList(ctx, out, a, root, g)
	}
	if c.selectorEligible(out, g) {
		return c.runInteractive(ctx, out, a, root, g)
	}
	return c.execute(ctx, out, a, root, g, c.Skills)
}

// selectorEligible reports whether the guided selection flow may open:
// interactive stdout, real stdin TTY, no machine-readable mode, and nothing
// that demands an unattended run. --dry-run does NOT disable the selector —
// an interactive dry run rehearses the flow and simulates on confirm
// (clarification #3).
func (c updateCmd) selectorEligible(out *Output, g Globals) bool {
	return out.Interactive() && !out.JSON() && stdinIsTTY() &&
		len(c.Skills) == 0 && !c.List && !g.Yes
}

// runInteractive discovers the plan, opens the selector over the actionable
// items, and applies exactly the confirmed selection (spec 018 US3). The
// application layer computed everything; the TUI only chooses names.
func (c updateCmd) runInteractive(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	// One discovery memo for discovery and the post-confirm execution, so
	// the selected skills' remote lookups are not repeated after confirm.
	ctx = app.WithDiscoveryMemo(ctx)
	pctx, done := out.withFetchProgress(ctx)
	plan, err := a.PlanUpdate(pctx, app.UpdatePlanRequest{
		Root: string(root), Offline: g.Offline, NoCache: g.NoCache,
	})
	done()
	if err != nil {
		return err
	}

	// Discovery failures must be as loud here as in the unattended path:
	// route the unchecked skills through execution so they surface as failed
	// rows with the partial exit code, exactly like a piped run.
	unchecked := uncheckedNames(plan)
	if len(unchecked) > 0 {
		out.Warn("%d skill(s) could not be checked and will be reported as failed: %s",
			len(unchecked), strings.Join(unchecked, ", "))
	}
	actionable := plan.Actionable()
	if len(actionable) == 0 {
		if len(unchecked) > 0 {
			return c.execute(ctx, out, a, root, g, unchecked)
		}
		// Nothing to choose from: report the terminal state instead of an
		// empty selector.
		return out.Result(renderUpdateList(out, plan, false), UpdateListJSON(plan, false))
	}

	items := make([]tui.UpdateItem, 0, len(actionable))
	for _, it := range actionable {
		items = append(items, tui.UpdateItem{
			Name: it.Name, Current: it.Current, Candidate: it.Candidate, Policy: it.Policy,
		})
	}
	// The fetch-progress renderer was shut down above: bubbletea owns the
	// terminal from here until the selection returns.
	sel, err := runUpdateSelectorFn(items, true)
	if err != nil {
		return err
	}
	switch {
	case sel.Interrupted:
		return errs.WithHint(errs.ErrCancelled, "no updates were applied")
	case sel.Cancelled || len(sel.Names) == 0:
		out.Info("Nothing selected; no updates applied.")
		return nil
	}
	if !out.Confirm(pluralSkills(len(sel.Names))+" selected. Continue?", g.Yes) {
		out.Info("No updates applied.")
		return nil
	}
	return c.execute(ctx, out, a, root, g, append(sel.Names, unchecked...))
}

// runList renders the read-only update report (spec 018 FR-006/FR-007): the
// canonical answer to "what would an update do", on stdout, changing nothing.
func (c updateCmd) runList(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	plan, err := a.PlanUpdate(ctx, app.UpdatePlanRequest{
		Root: string(root), Offline: g.Offline, NoCache: g.NoCache,
	})
	done()
	if err != nil {
		return err
	}
	if rErr := out.Result(renderUpdateList(out, plan, c.All), UpdateListJSON(plan, c.All)); rErr != nil {
		return rErr
	}
	return discoveryFailureErr(plan)
}

// discoveryFailureErr fails the command (after the report rendered) when any
// skill's eligibility could not be determined because remote discovery
// errored: a report that could not check its remotes must not exit like a
// verified one, or CI gates pass green during outages. A pin whose
// informational newest-tag lookup failed does NOT count — its eligibility is
// fully determined by the lock (the --all STATUS cell still says
// "lookup failed").
func discoveryFailureErr(plan app.UpdatePlan) error {
	unchecked := uncheckedNames(plan)
	if len(unchecked) == 0 {
		return nil
	}
	return errs.WithHint(
		fmt.Errorf("%w: %d skill(s) could not be checked for updates", errs.ErrSourceUnavailable, len(unchecked)),
		"check network access and the skills' source URLs",
	)
}

// uncheckedNames returns skills whose eligibility itself is unknown because
// discovery failed — not offline skips, and not pins whose informational
// lookup failed (a pin's eligibility never depended on the lookup).
func uncheckedNames(plan app.UpdatePlan) []string {
	var names []string
	for _, it := range plan.Items {
		if it.Status == app.StatusUnknown && it.DiscoveryErr != "" {
			names = append(names, it.Name)
		}
	}
	return names
}

// execute runs the update for names (command arguments, a confirmed
// interactive selection, or empty for every actionable skill) and renders the
// per-skill result.
func (c updateCmd) execute(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals, names []string) error {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	res, err := a.Update(ctx, app.UpdateRequest{
		Root: string(root), Names: names,
		Offline: g.Offline, NoCache: g.NoCache, DryRun: g.DryRun,
	})
	done()
	if err != nil && len(res.Skills) == 0 {
		// Nothing ran (missing lock, unknown name): the error alone tells the
		// story better than an empty summary would.
		return err
	}
	if rErr := out.Result(renderUpdateResult(out, res), UpdateResultJSON(res)); rErr != nil {
		return rErr
	}
	return err
}

// UpdateListJSON builds the --json object for an update report: one item per
// reported skill with candidate and status fields (contracts/cli-update.md).
func UpdateListJSON(plan app.UpdatePlan, all bool) map[string]any {
	items := plan.Actionable()
	if all {
		items = plan.Items
	}
	skills := make([]map[string]any, 0, len(items))
	for _, it := range items {
		skills = append(skills, map[string]any{
			"name":          it.Name,
			"source":        it.Source,
			"current":       it.Current,
			"candidate":     it.Candidate,
			"policy":        it.Policy,
			"status":        string(it.Status),
			"reason":        it.Reason,
			"informational": it.Informational,
		})
	}
	return map[string]any{
		"skills":            skills,
		"updates_available": len(plan.Actionable()),
		// not_checked distinguishes "verified current" from "could not be
		// determined" for machine consumers — without it, a total discovery
		// outage is byte-identical to a healthy project.
		"not_checked": countUnknown(plan),
	}
}

// UpdateResultJSON builds the --json object for an update execution. The
// historical fields (`changed`, `skills[].{name,content_hash,changed}`) are
// retained; from/to/status/outcome detail is additive (FR-016).
func UpdateResultJSON(res app.UpdateResult) map[string]any {
	skills := make([]map[string]any, 0, len(res.Skills))
	for _, s := range res.Skills {
		skills = append(skills, map[string]any{
			"name":         s.Name,
			"content_hash": s.ContentHash,
			"changed":      s.Outcome == app.UpdateOutcomeUpdated,
			"from":         s.From,
			"to":           s.To,
			"status":       string(s.Status),
			"outcome":      string(s.Outcome),
			"reason":       s.Reason,
		})
	}
	return map[string]any{
		"changed":   res.Changed,
		"skills":    skills,
		"updated":   res.Updated,
		"failed":    res.Failed,
		"no_change": res.NoChange,
		"dry_run":   res.DryRun,
	}
}

// updateSummary composes the one-line human run summary. It never asserts
// freshness that was not verified — a run without applied updates says what
// happened ("no changes"), not what state the project is in.
func updateSummary(res app.UpdateResult) string {
	if len(res.Skills) == 0 {
		return "No skills installed."
	}
	if res.Updated == 0 && res.Failed == 0 {
		return "No changes made; " + pluralSkills(res.NoChange) + " unchanged"
	}
	var parts []string
	if res.Updated > 0 {
		verb := "Updated"
		if res.DryRun {
			verb = "Would update"
		}
		parts = append(parts, verb+" "+pluralSkills(res.Updated))
	}
	if res.Failed > 0 {
		parts = append(parts, pluralSkills(res.Failed)+" failed")
	}
	if res.NoChange > 0 {
		parts = append(parts, pluralSkills(res.NoChange)+" unchanged")
	}
	return strings.Join(parts, ", ")
}

// pluralSkills renders "1 skill" / "N skills".
func pluralSkills(n int) string {
	if n == 1 {
		return "1 skill"
	}
	return strconv.Itoa(n) + " skills"
}
