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

// reportedError marks an error whose story the interactive UI already told
// (the wizard's result screen). Run maps it to its exit code without printing
// the generic "error: …" line a second time (spec 014 FR-020).
type reportedError struct{ err error }

func (e reportedError) Error() string { return e.err.Error() }
func (e reportedError) Unwrap() error { return e.err }

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
	ctx, events, done := out.withInstallProgress(ctx)
	defer done()
	req := c.request(root, g)
	req.Progress = events
	res, err := a.InstallFromLock(ctx, req)
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
			Execute: func(ctx context.Context, agentIDs []string, progress func(app.InstallProgressEvent)) (app.InstallFromLockResult, error) {
				req := c.request(root, g)
				req.Agents = agentIDs
				req.Progress = progress
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
		// The wizard's result screen already showed the detailed summary and
		// failure table (spec 014 FR-020): never print the generic summary
		// again. Two facts the screen does NOT carry are still reported on
		// stderr: pruned entries, and the spec-013 "removed for" notes — a
		// narrow-to-zero run deletes managed targets while its entries land
		// as "repaired", so without these lines a destructive run would read
		// as a plain repair.
		for _, p := range outcome.Result.Pruned {
			out.Info("pruned: %s", p)
		}
		for _, s := range outcome.Result.Skills {
			reportLockSkillAgentDiag(out, s, true)
		}
		if outcome.Err != nil {
			// The result table has no hint column; the remediation hint lives
			// only in the drill-down detail view, so persist it on stderr.
			if hint := errs.HintOf(outcome.Err); hint != "" {
				out.Hint("→ %s", hint)
			}
			return reportedError{err: outcome.Err}
		}
		return nil
	default:
		return outcome.Err
	}
}

// renderLockInstall prints the install result on the plain streams and the
// --json document (spec 014 US3): a summary whose counters always sum to the
// total (FR-015/FR-016), one block per unsuccessful skill with equivalent
// failure information (FR-021), and the additively-extended stable JSON
// shape (contracts/install-result-json.md). explicit reports whether the run
// had an explicit agent selection (spec 013 FR-014) — it gates the per-skill
// agentsKept/agentsAdded/agentsRemoved JSON fields and the "removed for" /
// dry-run agent plan lines.
func renderLockInstall(out *Output, res app.InstallFromLockResult, explicit bool) error {
	sum := app.Aggregate(res.Skills)
	skills := make([]map[string]any, 0, len(res.Skills))
	for _, s := range res.Skills {
		reportLockSkillAgentDiag(out, s, explicit)
		skills = append(skills, lockSkillJSONEntry(s, explicit))
	}
	for _, p := range res.Pruned {
		out.Info("pruned: %s", p)
	}
	doc := map[string]any{
		"changed":     res.Changed,
		"initialized": res.Initialized,
		"agents":      res.Agents,
		"status":      string(sum.Outcome),
		"summary":     summaryJSON(sum),
		"skills":      skills,
		"pruned":      res.Pruned,
	}
	return out.Result(humanLockInstall(res, sum), doc)
}

// summaryJSON serializes the counters; every counter is always present so
// consumers can rely on the keys (the planned counter appears only for dry
// runs, per the contract).
func summaryJSON(sum app.InstallSummary) map[string]any {
	m := map[string]any{
		"total":        sum.Total,
		"installed":    sum.Installed,
		"repaired":     sum.Repaired,
		"upToDate":     sum.UpToDate,
		"skipped":      sum.Skipped,
		"failed":       sum.Failed,
		"cancelled":    sum.Cancelled,
		"notAttempted": sum.NotAttempted,
	}
	if sum.Planned > 0 {
		m["planned"] = sum.Planned
	}
	return m
}

// humanLockInstall renders the plain result: headline, counters, per-failure
// blocks, and the dry-run plan list. Unknown values render — (FR-014); all
// untrusted text is sanitized (FR-028). The string carries no trailing
// newline (Result appends one).
func humanLockInstall(res app.InstallFromLockResult, sum app.InstallSummary) string {
	var b strings.Builder
	b.WriteString(tui.InstallHeadline(sum) + "\n")
	fmt.Fprintf(&b, "%d skills processed", sum.Total)
	if line := tui.InstallCounterLine(sum); line != "" {
		b.WriteString("\n" + line)
	}
	for _, s := range res.Skills {
		switch app.InstallStatus(s.Status) { //nolint:exhaustive // successful statuses render as counters only (clarification #2)
		case app.InstallStatusFailed, app.InstallStatusCancelled, app.InstallStatusNotAttempted:
			b.WriteString("\n\n" + failureBlock(s))
		default:
		}
	}
	if sum.Planned > 0 {
		b.WriteString("\n")
		for _, s := range res.Skills {
			if s.Status == app.LockSkillPlanned {
				fmt.Fprintf(&b, "\n%-20s %s", tui.PlannedTitle(s.PlannedAction), tui.Sanitize(s.Name))
			}
		}
		b.WriteString("\nnothing written (--dry-run)")
	}
	return b.String()
}

// failureBlock renders one unsuccessful skill's equivalent information
// (FR-021): status, skill, source, version, phase, reason, and hint.
func failureBlock(s app.LockSkillResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", strings.ToUpper(s.Status), tui.Sanitize(s.Name))
	fmt.Fprintf(&b, "  Source:   %s\n", tui.OrDash(s.Source))
	fmt.Fprintf(&b, "  Version:  %s\n", tui.OrDash(s.ResolvedVersion))
	fmt.Fprintf(&b, "  Phase:    %s", tui.OrDash(string(s.Phase)))
	if f := s.Failure; f != nil {
		fmt.Fprintf(&b, "\n  Category: %s", tui.Sanitize(string(f.Category)))
		fmt.Fprintf(&b, "\n  Reason:   %s", tui.OrDash(f.Message))
		if f.Hint != "" {
			fmt.Fprintf(&b, "\n  Hint:     %s", tui.Sanitize(f.Hint))
		}
	} else if s.Err != nil {
		fmt.Fprintf(&b, "\n  Reason:   %s", tui.Sanitize(s.Err.Error()))
	}
	return b.String()
}

// lockSkillJSONEntry builds one skill's --json entry: the frozen legacy
// fields, the spec 014 additive provenance/failure fields (unknown scalars
// omitted, never fabricated — FR-014's JSON analogue), and the
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
	putIf := func(key, val string) {
		if val != "" {
			entry[key] = val
		}
	}
	putIf("sourceType", s.SourceType)
	putIf("skillPath", s.SkillPath)
	putIf("requestedRef", s.RequestedRef)
	putIf("resolvedVersion", s.ResolvedVersion)
	putIf("resolvedRef", s.ResolvedRef)
	putIf("commit", s.Commit)
	putIf("installMode", s.InstallMode)
	putIf("phase", string(s.Phase))
	putIf("plannedAction", s.PlannedAction)
	if len(s.Agents) > 0 {
		entry["agents"] = s.Agents
	}
	if f := s.Failure; f != nil {
		fj := map[string]any{
			"category": string(f.Category),
			"message":  f.Message,
		}
		if f.Phase != "" {
			fj["phase"] = string(f.Phase)
		}
		if f.Hint != "" {
			fj["hint"] = f.Hint
		}
		if f.Expected != "" || f.Actual != "" {
			fj["expected"] = f.Expected
			fj["actual"] = f.Actual
		}
		entry["failure"] = fj
	}
	// An unsuccessful skill's AgentsKept/Added/Removed describe intent, not
	// outcome (removeDroppedAgents' two-phase check guarantees nothing was
	// actually removed when a skill fails, and a cancelled/not-attempted
	// entry never reached the removal at all) — omit rather than report
	// values a consumer could mistake for what happened.
	if explicit && agentDiffReportable(s.Status) {
		entry["agentsKept"] = nonNilStrings(s.AgentsKept)
		entry["agentsAdded"] = nonNilStrings(s.AgentsAdded)
		entry["agentsRemoved"] = nonNilStrings(s.AgentsRemoved)
	}
	return entry
}

// agentDiffReportable reports whether a status means the skill's agent diff
// actually happened: only statuses that completed their pipeline qualify.
// failed, cancelled, and not-attempted entries carry the diff as unexecuted
// intent (spec 014 review C7).
func agentDiffReportable(status string) bool {
	switch app.InstallStatus(status) { //nolint:exhaustive // pending/running never appear in final results
	case app.InstallStatusInstalled, app.InstallStatusRepaired,
		app.InstallStatusUpToDate, app.InstallStatusSkipped, app.InstallStatusPlanned:
		return true
	default:
		return false
	}
}

// reportLockSkillAgentDiag writes the human-readable "removed for" note and,
// for a --dry-run plan, the keep/add/remove/lock preview lines — both gated
// on an explicit agent selection.
func reportLockSkillAgentDiag(out *Output, s app.LockSkillResult, explicit bool) {
	if !explicit || !agentDiffReportable(s.Status) {
		// An unsuccessful skill's AgentsKept/Added/Removed describe what was
		// intended, not what happened — the two-phase removal in
		// removeDroppedAgents guarantees nothing was actually removed when
		// the skill fails (or is cancelled before reaching it), so reporting
		// "removed for" here would tell the user something was deleted when
		// it wasn't.
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
