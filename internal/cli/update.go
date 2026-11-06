package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/tui"
)

// runUpdateTableFn indirects the grouped update table so tests can stub the
// TUI (the runWizardFn pattern).
var runUpdateTableFn = tui.SelectGroupedUpdates

// updateCmd advances skills to the newest version within their constraints.
type updateCmd struct {
	Skills []string `arg:"" optional:"" help:"Skills to update (default: all with available updates)."`
	Repo   []string `name:"repo" sep:"none" placeholder:"REPOSITORY" help:"Update the skills installed from this repository (repeatable)."`
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
		"gskill update --repo acme/skills",
		"gskill update --list --repo acme/skills",
	)
}

// Run executes `gskill update`.
func (c updateCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	if c.All && !c.List {
		return errs.New(errs.CodeUsage, "--all requires --list")
	}
	if c.List && len(c.Skills) > 0 {
		return errs.New(errs.CodeUsage, "--list reports the whole project or the --repo groups and does not take skill names")
	}
	if c.List {
		return c.runList(ctx, out, a, root, g)
	}
	if c.selectorEligible(out, g) {
		return c.runInteractive(ctx, out, a, root, g)
	}
	_, err := c.execute(ctx, out, a, root, g, c.Skills)
	return err
}

// selectorEligible reports whether the guided selection flow may open:
// interactive stdout, real stdin TTY, no machine-readable mode, and nothing
// that demands an unattended run. --dry-run does NOT disable the selector —
// an interactive dry run rehearses the flow and simulates on confirm
// (clarification #3).
func (c updateCmd) selectorEligible(out *Output, g Globals) bool {
	return out.Interactive() && !out.JSON() && stdinIsTTY() &&
		len(c.Skills) == 0 && len(c.Repo) == 0 && !c.List && !g.Yes
}

// runInteractive runs the grouped update table as a session of rounds (spec
// 032 US1). Each round plans, lets the user pick in the table and confirm in
// its review, and applies exactly that selection through execute, outside the
// TUI, so the run keeps the fetch progress, safe cancellation, and grouped
// results of an unattended update. The table then reopens on the refreshed
// plan with the selection that is still eligible.
func (c updateCmd) runInteractive(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	// One discovery memo for the whole session: a refresh re-reads the
	// project files but asks no remote again, and failures stay failures.
	ctx = app.WithDiscoveryMemo(ctx)
	var (
		selected       []string
		cursor, banner string
		sessionErr     error
		ran            bool
	)
	for round := 0; ; round++ {
		pctx, done := out.withFetchProgress(ctx)
		plan, err := a.PlanUpdate(pctx, app.UpdatePlanRequest{
			Root: string(root), Offline: g.Offline, NoCache: g.NoCache,
		})
		done()
		if err != nil {
			return err
		}
		if len(plan.Items) == 0 {
			return out.Result(renderUpdateList(out, plan, false), UpdateListJSON(plan, false))
		}
		// Discovery failures must be as loud here as in the unattended path:
		// the table shows them as failed, and every round runs them so they
		// surface as failed rows with the partial exit code.
		unchecked := uncheckedNames(plan)
		if round == 0 && len(unchecked) > 0 {
			out.Warn("%d skill(s) could not be checked and will be reported as failed: %s",
				len(unchecked), strings.Join(unchecked, ", "))
		}

		// The fetch-progress renderer was shut down above: bubbletea owns the
		// terminal from here until the round returns.
		sel, err := runUpdateTableFn(updateTableFor(plan, selected, cursor, banner, g.DryRun), true)
		if err != nil {
			return err
		}
		if end, endErr := sessionEnd(out, sel, ran, sessionErr); end {
			return endErr
		}

		res, err := c.execute(ctx, out, a, root, g, append(sel.Names, unchecked...))
		ran = true
		if err != nil {
			// A partial round is reported and the session goes on; anything
			// else (nothing ran, or the run was cancelled) ends it.
			if len(res.Skills) == 0 || !errors.Is(err, errs.ErrPartialInstall) {
				return err
			}
			if sessionErr == nil {
				sessionErr = err
			}
		}
		selected, cursor, banner = sel.Names, sel.Cursor, updateSummary(res)
	}
}

// sessionEnd reports whether a table round ends the session, and with what:
// ctrl-c exits 130, and leaving the table (esc, q, or enter with nothing
// selected) returns the first failed round's error, or nil (spec 032 R6).
func sessionEnd(out *Output, sel tui.UpdateTableResult, ran bool, sessionErr error) (bool, error) {
	switch {
	case sel.Interrupted && !ran:
		return true, errs.WithHint(errs.ErrCancelled, "no updates were applied")
	case sel.Interrupted:
		return true, errs.ErrCancelled
	case sel.Cancelled:
		return true, sessionErr
	case len(sel.Names) == 0:
		out.Info("Nothing selected; no updates applied.")
		return true, sessionErr
	default:
		return false, nil
	}
}

// updateTableFor maps the plan onto one table round. Groups, counts, and
// status words come from the same helpers as the text report, so the table
// and `update --list --all` say the same thing (spec 032 FR-005).
func updateTableFor(plan app.UpdatePlan, selected []string, cursor, banner string, dryRun bool) tui.UpdateTable {
	tbl := tui.UpdateTable{Selected: selected, Cursor: cursor, Banner: banner, DryRun: dryRun}
	for _, grp := range plan.Groups() {
		tg := tui.TableGroup{
			Key:     string(grp.Repo.Kind) + "\x00" + grp.Repo.Key,
			Label:   grp.Repo.Label,
			Local:   grp.Repo.Kind == source.RepoLocal,
			Summary: groupLine(grp.Repo.Label, grp.Available(), grp.Total(), grp.Failed(), grp.NotChecked()),
		}
		for _, it := range grp.Items {
			tg.Rows = append(tg.Rows, tui.TableRow{
				Name: it.Name, Path: it.Path, Current: it.Current, Candidate: it.Candidate, Policy: it.Policy,
				Status: updateStatusCell(it), Detail: tableDetail(it), Eligible: it.Actionable(),
			})
		}
		tbl.Groups = append(tbl.Groups, tg)
	}
	return tbl
}

// tableDetail is the table's line for the row under the cursor: the report's
// guidance, else the transition or the reason, plus the in-repository path.
func tableDetail(it app.UpdatePlanItem) string {
	d := updateListDetail(it)
	switch {
	case d != "":
	case it.Actionable():
		d = it.Current + " → " + it.Candidate + " within " + it.Policy
	default:
		d = it.Reason
	}
	if it.Path == "" {
		return d
	}
	if d == "" {
		return "path: " + it.Path
	}
	return d + " (path: " + it.Path + ")"
}

// runList renders the read-only update report (spec 018 FR-006/FR-007): the
// canonical answer to "what would an update do", on stdout, changing nothing.
func (c updateCmd) runList(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	plan, err := a.PlanUpdate(ctx, app.UpdatePlanRequest{
		Root: string(root), Repos: c.Repo, Offline: g.Offline, NoCache: g.NoCache,
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
		if it.Status == app.StatusLookupFailed && it.DiscoveryErr != "" {
			names = append(names, it.Name)
		}
	}
	return names
}

// execute runs the update for names (command arguments, a confirmed
// interactive selection, or empty for every actionable skill), renders the
// per-skill result, and returns it for the interactive session's next round.
func (c updateCmd) execute(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals, names []string) (app.UpdateResult, error) {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	res, err := a.Update(ctx, app.UpdateRequest{
		Root: string(root), Names: names, Repos: c.Repo,
		Offline: g.Offline, NoCache: g.NoCache, DryRun: g.DryRun,
	})
	done()
	if err != nil && len(res.Skills) == 0 {
		// Nothing ran (missing lock, unknown name): the error alone tells the
		// story better than an empty summary would.
		return res, err
	}
	if rErr := out.Result(renderUpdateResult(out, res), UpdateResultJSON(res)); rErr != nil {
		return res, rErr
	}
	return res, err
}

// UpdateListJSON builds the --json object for an update report: one item per
// reported skill with candidate and status fields (contracts/cli-update.md).
func UpdateListJSON(plan app.UpdatePlan, all bool) map[string]any {
	items := plan.Actionable()
	if all {
		items = plan.Items
	}
	repos := planRepos(plan)
	skills := make([]map[string]any, 0, len(items))
	for _, it := range items {
		skills = append(skills, map[string]any{
			"name":          it.Name,
			"repository":    repos[it.Name].Label,
			"local":         repos[it.Name].Kind == source.RepoLocal,
			"source":        it.Source,
			"current":       it.Current,
			"candidate":     it.Candidate,
			"policy":        it.Policy,
			"status":        string(it.Status),
			"reason":        it.Reason,
			"informational": it.Informational,
			"shape":         string(it.Shape),
			"pinned":        it.Pinned,
			"next_action":   it.NextAction,
		})
	}
	return map[string]any{
		"skills":            skills,
		"updates_available": len(plan.Actionable()),
		"pinned":            countPinned(plan),
		// not_checked distinguishes "verified current" from "could not be
		// determined" for machine consumers — without it, a total discovery
		// outage is byte-identical to a healthy project.
		"not_checked": countUnknown(plan),
	}
}

// planRepos maps each skill to its group's repository, so JSON names the
// group's label and equal strings always mean the same group (spec 032
// FR-030).
func planRepos(plan app.UpdatePlan) map[string]source.Repo {
	repos := make(map[string]source.Repo, len(plan.Items))
	for _, g := range plan.Groups() {
		for _, it := range g.Items {
			repos[it.Name] = g.Repo
		}
	}
	return repos
}

// countPinned counts the items only a change of declared intent can move.
func countPinned(plan app.UpdatePlan) int {
	n := 0
	for _, it := range plan.Items {
		if it.Pinned {
			n++
		}
	}
	return n
}

// UpdateResultJSON builds the --json object for an update execution. The
// historical fields (`changed`, `skills[].{name,content_hash,changed}`) are
// retained; from/to/status/outcome detail is additive (FR-016).
func UpdateResultJSON(res app.UpdateResult) map[string]any {
	repos := map[string]source.Repo{}
	for _, g := range res.Groups() {
		for _, s := range g.Skills {
			repos[s.Name] = g.Repo
		}
	}
	skills := make([]map[string]any, 0, len(res.Skills))
	for _, s := range res.Skills {
		skills = append(skills, map[string]any{
			"name":         s.Name,
			"repository":   repos[s.Name].Label,
			"local":        repos[s.Name].Kind == source.RepoLocal,
			"content_hash": s.ContentHash,
			"changed":      s.Outcome == app.UpdateOutcomeUpdated,
			"from":         s.From,
			"to":           s.To,
			"status":       string(s.Status),
			"outcome":      string(s.Outcome),
			"reason":       s.Reason,
			"pinned":       s.Status.Pinned(),
			"next_action":  s.NextAction,
		})
	}
	return map[string]any{
		"changed":    res.Changed,
		"skills":     skills,
		"updated":    res.Updated,
		"failed":     res.Failed,
		"no_change":  res.NoChange,
		"pinned":     res.Pinned,
		"up_to_date": res.UpToDate,
		"local":      res.Local,
		"dry_run":    res.DryRun,
	}
}

// updateSummary composes the one-line human run summary (spec 024 FR-009).
// It counts every outcome — updated, pinned, up to date, local, failed — so a
// run that moved nothing says why, and never reads as an error.
func updateSummary(res app.UpdateResult) string {
	if len(res.Skills) == 0 {
		return noSkillsDeclared
	}
	var parts []string
	switch {
	case res.Updated > 0 && res.DryRun:
		parts = append(parts, "Would update "+pluralSkills(res.Updated))
	case res.Updated > 0:
		parts = append(parts, "Updated "+pluralSkills(res.Updated))
	case res.Failed == 0:
		parts = append(parts, "No changes made")
	}
	if res.Pinned > 0 {
		parts = append(parts, strconv.Itoa(res.Pinned)+" pinned")
	}
	if res.UpToDate > 0 {
		parts = append(parts, strconv.Itoa(res.UpToDate)+" up to date")
	}
	if res.Local > 0 {
		parts = append(parts, strconv.Itoa(res.Local)+" local")
	}
	if other := res.NoChange - res.Pinned - res.UpToDate - res.Local; other > 0 {
		parts = append(parts, strconv.Itoa(other)+" not checked")
	}
	if res.Failed > 0 {
		parts = append(parts, pluralSkills(res.Failed)+" failed")
	}
	return strings.Join(parts, " · ")
}

// pluralSkills renders "1 skill" / "N skills".
func pluralSkills(n int) string {
	if n == 1 {
		return "1 skill"
	}
	return strconv.Itoa(n) + " skills"
}
