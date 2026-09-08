package cli

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/tui"
)

// runUpgradeSelectorFn indirects the interactive selector so tests can stub
// the TUI.
var runUpgradeSelectorFn = tui.SelectUpgrades

// upgradeCmd moves a skill's declared version in skills.toml and realizes it
// (spec 024 US3): upgrade = change intent + resolve + install.
type upgradeCmd struct {
	Args   []string `arg:"" optional:"" name:"skill" help:"Skills to upgrade. With exactly one skill, a second argument is the target version (same as --to)."`
	To     string   `help:"Exact target version or tag; must exist as a release of the skill's source."`
	Latest bool     `help:"Move to the newest stable release beyond the current declaration (the default)."`
	All    bool     `help:"Without skill names and outside a terminal: upgrade every upgradable skill."`
}

// Help returns the detailed help shown by `gskill upgrade --help`.
func (upgradeCmd) Help() string {
	return describedHelp(
		"upgrade changes what skills.toml declares, then resolves, locks, installs, and verifies "+
			"the result. update never edits skills.toml; upgrade is how a declared version moves.",
		"gskill upgrade my-skill",
		"gskill upgrade my-skill --latest",
		"gskill upgrade my-skill 2.1.0",
		"gskill upgrade my-skill --to 2.1.0",
		"gskill --no-interactive upgrade --all",
		"gskill --dry-run upgrade my-skill",
	)
}

// Run executes `gskill upgrade`.
func (c upgradeCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	if g.Offline {
		return errs.WithHint(errs.New(errs.CodeUsage, "upgrade needs the remote to discover releases"),
			"drop --offline, or edit skills.toml and run `gskill install`")
	}
	names, to, err := c.parseArgs(a, root)
	if err != nil {
		return err
	}
	req := app.UpgradeRequest{
		Root: string(root), Names: names, To: to, Latest: c.Latest || to == "",
		Offline: g.Offline, NoCache: g.NoCache, DryRun: g.DryRun,
	}
	if len(names) == 0 && c.selectorEligible(out, g) {
		return c.runInteractive(ctx, out, a, req, g)
	}
	if len(names) == 0 && !c.All {
		return errs.WithHint(errs.New(errs.CodeUsage, "upgrade rewrites declared intent: name the skills to move, or pass --all"),
			"run `gskill update --list --all` to see which skills can move")
	}
	return c.execute(ctx, out, a, req)
}

// parseArgs splits the positionals into skill names and an optional target
// version: `upgrade <skill> <version>` is sugar for --to when exactly one
// skill is named and the second token is not itself a declared skill.
func (c upgradeCmd) parseArgs(a *app.App, root projectRoot) ([]string, string, error) {
	names, to := c.Args, c.To
	if len(names) == 2 && to == "" && !slices.Contains(a.DeclaredSkillNames(string(root)), names[1]) {
		names, to = names[:1], names[1]
	}
	if to != "" && len(names) != 1 {
		return nil, "", errs.New(errs.CodeUsage, "--to applies to exactly one skill")
	}
	if to != "" && c.Latest {
		return nil, "", errs.New(errs.CodeUsage, "--to and --latest are mutually exclusive")
	}
	return names, to, nil
}

func (c upgradeCmd) selectorEligible(out *Output, g Globals) bool {
	return out.Interactive() && !out.JSON() && stdinIsTTY() && !c.All && !g.Yes
}

// runInteractive plans every skill, offers the upgradable ones, and applies
// exactly the confirmed selection.
func (c upgradeCmd) runInteractive(ctx context.Context, out *Output, a *app.App, req app.UpgradeRequest, g Globals) error {
	ctx = app.WithDiscoveryMemo(ctx)
	pctx, done := out.withFetchProgress(ctx)
	plan, err := a.PlanUpgrade(pctx, req)
	done()
	if err != nil {
		return err
	}
	candidates := plan.Upgradable()
	if len(candidates) == 0 {
		out.Info("Nothing to upgrade: every skill is already at its newest release or cannot be moved by upgrade.")
		return nil
	}
	items := make([]tui.UpdateItem, 0, len(candidates))
	for _, it := range candidates {
		items = append(items, tui.UpdateItem{
			Name: it.Name, Current: it.Current, Candidate: candidateText(it),
			Declaration: it.CurrentDecl + " → " + declAfter(it),
		})
	}
	sel, err := runUpgradeSelectorFn(items, true)
	if err != nil {
		return err
	}
	switch {
	case sel.Interrupted:
		return errs.WithHint(errs.ErrCancelled, "no upgrades were applied")
	case sel.Cancelled || len(sel.Names) == 0:
		out.Info("Nothing selected; no upgrades applied.")
		return nil
	}
	if !out.Confirm(pluralSkills(len(sel.Names))+" selected. Rewrite skills.toml and upgrade?", g.Yes) {
		out.Info("No upgrades applied.")
		return nil
	}
	req.Names = sel.Names
	return c.execute(ctx, out, a, req)
}

func candidateText(it app.UpgradePlanItem) string {
	switch {
	case it.Candidate.Version != "":
		return it.Candidate.Version
	case it.Candidate.Tag != "":
		return it.Candidate.Tag
	default:
		return it.Candidate.Commit
	}
}

func declAfter(it app.UpgradePlanItem) string {
	if it.NewDecl != "" {
		return it.NewDecl
	}
	return it.CurrentDecl
}

func (c upgradeCmd) execute(ctx context.Context, out *Output, a *app.App, req app.UpgradeRequest) error {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	res, err := a.Upgrade(ctx, req)
	done()
	if err != nil && len(res.Skills) == 0 {
		return err
	}
	if rErr := out.Result(renderUpgradeResult(out, res), UpgradeJSON(res)); rErr != nil {
		return rErr
	}
	return err
}

// UpgradeJSON builds the --json object for an upgrade run (contracts/cli.md).
func UpgradeJSON(res app.UpgradeResult) map[string]any {
	skills := make([]map[string]any, 0, len(res.Skills))
	for _, s := range res.Skills {
		skills = append(skills, map[string]any{
			"name":               s.Name,
			"shape":              string(s.Shape),
			"from":               s.From,
			"to":                 s.To,
			"declaration_before": s.DeclBefore,
			"declaration_after":  s.DeclAfter,
			"action":             string(s.Action),
			"outcome":            string(s.Outcome),
			"reason":             s.Reason,
		})
	}
	return map[string]any{
		"upgraded":  res.Upgraded,
		"unchanged": res.Unchanged,
		"refused":   res.Refused,
		"failed":    res.Failed,
		"changed":   res.Changed,
		"dry_run":   res.DryRun,
		"skills":    skills,
	}
}

// upgradeSummary composes the run summary line.
func upgradeSummary(res app.UpgradeResult) string {
	if len(res.Skills) == 0 {
		return noSkillsDeclared
	}
	var parts []string
	switch {
	case res.Upgraded > 0 && res.DryRun:
		parts = append(parts, "Would upgrade "+pluralSkills(res.Upgraded))
	case res.Upgraded > 0:
		parts = append(parts, "Upgraded "+pluralSkills(res.Upgraded)+"; skills.toml and skills-lock.json updated; verified")
	case res.Failed == 0 && res.Refused == 0:
		parts = append(parts, "No changes made")
	}
	if res.Unchanged > 0 {
		parts = append(parts, strconv.Itoa(res.Unchanged)+" unchanged")
	}
	if res.Refused > 0 {
		parts = append(parts, strconv.Itoa(res.Refused)+" refused")
	}
	if res.Failed > 0 {
		parts = append(parts, strconv.Itoa(res.Failed)+" failed; everything was rolled back")
	}
	return strings.Join(parts, " · ")
}
