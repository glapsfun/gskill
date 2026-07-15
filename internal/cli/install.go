package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/tui"
)

// runLockWizardFn is swapped in tests.
var runLockWizardFn = tui.RunLockWizard

// installCmd restores the environment declared in skills-lock.json.
type installCmd struct {
	Agent          []string `sep:"," help:"Target agent(s); repeatable or comma-separated. This is the exact desired set: it replaces (not merges with) each skill's currently recorded agents — list every agent you want kept, not just the one being added."`
	Copy           bool     `help:"Copy instead of symlinking (deprecated alias for --install-mode copy)."`
	InstallMode    string   `name:"install-mode" placeholder:"auto|symlink|copy" help:"How skills are placed into agent directories."`
	Force          bool     `help:"Accept changed upstream content: reinstall and rewrite the recorded computedHash."`
	NoInit         bool     `name:"no-init" help:"Never auto-initialize the project; fail instead."`
	FrozenLockfile bool     `name:"frozen-lockfile" help:"Restore exactly from the lockfile; never modify it."`
	Prune          bool     `help:"Remove managed installs whose lock entries are gone."`
}

// Help returns the detailed help shown by `gskill install --help`.
func (installCmd) Help() string {
	return examplesHelp(
		"gskill install",
		"gskill install --agent claude,codex",
		"gskill install --frozen-lockfile",
	)
}

// installModes bounds --install-mode.
var installModes = map[string]bool{"": true, "auto": true, "symlink": true, "copy": true}

// validateFlags rejects contradictory or invalid flag combinations (exit 2).
func (c installCmd) validateFlags() error {
	if c.Force && c.FrozenLockfile {
		return fmt.Errorf("%w: --force cannot be combined with --frozen-lockfile (frozen runs never rewrite the lock)", errs.ErrUsage)
	}
	if c.Prune && c.FrozenLockfile {
		return fmt.Errorf("%w: --prune cannot be combined with --frozen-lockfile (pruning modifies installed state)", errs.ErrUsage)
	}
	if !installModes[c.InstallMode] {
		return fmt.Errorf("%w: invalid --install-mode %q (want auto, symlink, or copy)", errs.ErrUsage, c.InstallMode)
	}
	if c.Agent != nil && len(c.Agent) == 0 {
		// Kong parses a bare `--agent=` into a non-nil, empty []string —
		// indistinguishable at the app layer from a deliberate TUI
		// zero-agent narrowing (spec 013 FR-012), which removes every
		// managed target for every skill with no confirmation outside the
		// wizard. The CLI has no syntax for that explicit-empty selection;
		// reject it here instead of silently wiping the project.
		return errs.WithHint(
			fmt.Errorf("%w: --agent requires at least one agent ID", errs.ErrUsage),
			"to remove every agent from a skill, use the interactive wizard (run 'gskill install' with no --agent)")
	}
	return nil
}

// mode merges --install-mode with the deprecated --copy alias.
func (c installCmd) mode() string {
	if c.InstallMode != "" {
		return c.InstallMode
	}
	return modeFromFlags(c.Copy, false, false)
}

// Run executes `gskill install`: restore every skill declared in
// skills-lock.json. The --offline, --dry-run, --yes, and --no-cache flags are
// global. In an interactive terminal a project whose lock needs agent
// selection opens the guided flow (spec 012 FR-013); --agent, --yes, --json,
// a non-TTY, and --frozen-lockfile all suppress it.
func (c installCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	if err := c.validateFlags(); err != nil {
		return err
	}
	if c.wizardEligible(out, g) {
		preview, found, err := a.PreviewLock(string(root))
		if err != nil {
			return err
		}
		if found && len(preview.Skills) > 0 {
			return c.runLockWizard(ctx, out, a, string(root), preview, g)
		}
	}
	return c.runLockDirect(ctx, out, a, string(root), g)
}

// request assembles the app request from the flags; every execution path
// (direct and wizard) builds from this one place so a new flag cannot be
// silently dropped by one of them.
func (c installCmd) request(root string, g Globals) app.InstallFromLockRequest {
	return app.InstallFromLockRequest{
		Root:        root,
		Agents:      c.Agent,
		InstallMode: c.mode(),
		NoInit:      c.NoInit,
		Force:       c.Force,
		DryRun:      g.DryRun,
		Offline:     g.Offline,
		Frozen:      c.FrozenLockfile,
		Prune:       c.Prune,
	}
}

// wizardEligible reports whether the guided lock-install flow may open:
// interactive stdout, real stdin TTY, no machine-readable mode, and no flag
// that demands an unattended run (research R4 matrix).
func (c installCmd) wizardEligible(out *Output, g Globals) bool {
	return out.Interactive() && !out.JSON() && stdinIsTTY() &&
		len(c.Agent) == 0 && !g.Yes && !g.DryRun && !c.FrozenLockfile
}

// runLockDirect executes the non-interactive install.
func (c installCmd) runLockDirect(ctx context.Context, out *Output, a *app.App, root string, g Globals) error {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	res, err := a.InstallFromLock(ctx, c.request(root, g))
	done()
	if err != nil && len(res.Skills) == 0 {
		// Nothing ran (missing/invalid lock, agent selection failed): report
		// the error alone instead of a misleading empty summary.
		return err
	}
	// An explicit --agent selection (c.Agent != nil) is what makes the run's
	// kept/added/removed agent breakdown meaningful (spec 013 FR-014); with
	// no --agent flag it stays unreported, matching req.Agents == nil.
	if renderErr := renderLockInstall(out, res, c.Agent != nil); renderErr != nil {
		return renderErr
	}
	return err
}

// runLockWizard drives the guided flow and maps its outcome onto the CLI
// contract: cancel → exit 130 with zero writes, no agents → exit 9 with zero
// writes, otherwise the summary (and any partial-failure error) of the run.
func (c installCmd) runLockWizard(ctx context.Context, out *Output, a *app.App, root string, preview app.LockPreview, g Globals) error {
	skills := make([]tui.LockWizardSkill, 0, len(preview.Skills))
	for _, s := range preview.Skills {
		skills = append(skills, tui.LockWizardSkill{Name: s.Name, Source: s.Source, Agents: s.Agents})
	}
	cfg := tui.LockWizardConfig{
		LockPath: preview.Path,
		Skills:   skills,
		Phases: tui.LockWizardPhases{
			Agents: func(ctx context.Context) ([]app.AgentChoice, error) {
				return a.AgentChoices(ctx, root)
			},
			Execute: func(ctx context.Context, agentIDs []string) (app.InstallFromLockResult, error) {
				req := c.request(root, g)
				req.Agents = agentIDs
				return a.InstallFromLock(ctx, req)
			},
		},
	}
	outcome, err := runLockWizardFn(ctx, cfg, out.Interactive())
	if err != nil {
		return err
	}
	switch {
	case outcome.Cancelled:
		return fmt.Errorf("%w — nothing was changed", errs.ErrCancelled)
	case outcome.NoAgents:
		return errs.WithHint(
			fmt.Errorf("%w: no supported agents detected", errs.ErrUnsupportedAgent),
			"pass --agent <id>[,<id>...] to install for a specific agent")
	case outcome.Executed:
		// The wizard's confirmed agent selection is always explicit (spec 013
		// FR-005/FR-006), including a zero-agent narrowing (FR-012/FR-017).
		if renderErr := renderLockInstall(out, outcome.Result, true); renderErr != nil {
			return renderErr
		}
		return outcome.Err
	default:
		return outcome.Err
	}
}

// renderLockInstall prints the install summary on the plain streams (so it
// survives the alternate screen) and the --json document. explicit reports
// whether the run had an explicit agent selection (--agent or a TUI
// confirmation, spec 013 FR-014) — it gates the per-skill agentsKept/
// agentsAdded/agentsRemoved JSON fields and the "removed for" / dry-run plan
// lines, all three emitted together (never one dropped for being empty) so a
// plain `gskill install` run's output is unaffected (contracts/
// cli-install-agent-replace.md).
func renderLockInstall(out *Output, res app.InstallFromLockResult, explicit bool) error {
	skills := make([]map[string]any, 0, len(res.Skills))
	var names []string
	installed, failed, planned := 0, 0, 0
	for _, s := range res.Skills {
		entry := lockSkillJSONEntry(s, explicit)
		reportLockSkillAgentDiag(out, s, explicit)
		skills = append(skills, entry)
		names = append(names, s.Name)
		switch s.Status {
		case app.LockSkillFailed:
			failed++
		case app.LockSkillInstalled, app.LockSkillRepaired:
			installed++
		case app.LockSkillPlanned:
			planned++
		}
	}
	human := fmt.Sprintf("Installed %d skill(s) for %d agent(s)", installed, len(res.Agents))
	switch {
	case planned > 0:
		human = fmt.Sprintf("Plan: would install %d skill(s) (%s) — nothing written (--dry-run)",
			planned, strings.Join(names, ", "))
	case failed > 0:
		human = fmt.Sprintf("Installed %d skill(s), %d failed", installed, failed)
	case !res.Changed:
		human = fmt.Sprintf("Up to date (%d skill(s), no changes)", len(res.Skills))
	case explicit && installed > 0 && len(res.Agents) == 0:
		// A genuine narrow-to-zero (spec 013 FR-012/FR-017): every managed
		// target was removed, nothing was installed, so "Installed ... for 0
		// agent(s)" would read as self-contradictory.
		human = fmt.Sprintf("Removed all agents from %d skill(s)", installed)
	}
	for _, p := range res.Pruned {
		out.Info("pruned: %s", p)
	}
	human = out.summary(human)
	return out.Result(human, map[string]any{
		"changed":     res.Changed,
		"initialized": res.Initialized,
		"agents":      res.Agents,
		"skills":      skills,
		"pruned":      res.Pruned,
	})
}

// lockSkillJSONEntry builds one skill's --json entry, including the
// agentsKept/agentsAdded/agentsRemoved fields when explicit (spec 013
// FR-014).
func lockSkillJSONEntry(s app.LockSkillResult, explicit bool) map[string]any {
	entry := map[string]any{
		"name":         s.Name,
		"source":       s.Source,
		"status":       s.Status,
		"computedHash": s.ComputedHash,
	}
	if s.Err != nil {
		entry["error"] = s.Err.Error()
	}
	// A failed skill's AgentsKept/Added/Removed describe intent, not outcome
	// (removeDroppedAgents' two-phase check guarantees nothing was actually
	// removed when a skill fails) — omit rather than report values a
	// consumer could mistake for what happened.
	if explicit && s.Status != app.LockSkillFailed {
		entry["agentsKept"] = nonNilStrings(s.AgentsKept)
		entry["agentsAdded"] = nonNilStrings(s.AgentsAdded)
		entry["agentsRemoved"] = nonNilStrings(s.AgentsRemoved)
	}
	return entry
}

// reportLockSkillAgentDiag writes the human-readable "removed for" note and,
// for a --dry-run plan, the keep/add/remove/lock preview lines — both gated
// on an explicit agent selection.
func reportLockSkillAgentDiag(out *Output, s app.LockSkillResult, explicit bool) {
	if !explicit || s.Status == app.LockSkillFailed {
		// A failed skill's AgentsKept/Added/Removed describe what was
		// intended, not what happened — the two-phase removal in
		// removeDroppedAgents guarantees nothing was actually removed when
		// the skill fails, so reporting "removed for" here would tell the
		// user something was deleted when it wasn't.
		return
	}
	if len(s.AgentsRemoved) > 0 {
		out.Info("%s: removed for %s", s.Name, strings.Join(s.AgentsRemoved, ", "))
	}
	if s.Status != app.LockSkillPlanned {
		return
	}
	for _, line := range lockAgentPlanLines(s.Name, s.AgentsKept, s.AgentsAdded, s.AgentsRemoved) {
		out.Info("%s", line)
	}
}

// nonNilStrings returns s unchanged, or an empty (non-nil) slice for nil —
// so a JSON-encoded "kept"/"added"/"removed" field is always "[]", never
// "null", for a skill whose diff on that axis happens to be empty.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// lockAgentPlanLines formats one skill's --dry-run agent plan, per
// contracts/cli-install-agent-replace.md's "--dry-run output" section: which
// agents are kept/added/removed, and the resulting before/after lock value.
func lockAgentPlanLines(name string, kept, added, removed []string) []string {
	before := sortedUnion(kept, removed)
	after := sortedUnion(kept, added)
	lines := []string{name}
	if len(kept) > 0 {
		lines = append(lines, "  keep:    "+strings.Join(kept, ", "))
	}
	if len(added) > 0 {
		lines = append(lines, "  add:     "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		lines = append(lines, "  remove:  "+strings.Join(removed, ", "))
	}
	lines = append(lines, fmt.Sprintf("  lock:    agents [%s] -> [%s]",
		strings.Join(before, ", "), strings.Join(after, ", ")))
	return lines
}

// sortedUnion returns the sorted union of a and b (a and b are already
// disjoint by construction — kept/added/removed partition the before/after
// agent sets).
func sortedUnion(a, b []string) []string {
	out := append([]string{}, a...)
	out = append(out, b...)
	sort.Strings(out)
	return out
}
