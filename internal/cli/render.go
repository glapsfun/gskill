package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/tui"
)

// Styled human output for interactive terminals (design 2026-07-08):
// kubectl-style aligned columns, semantic status glyphs, no borders. The
// plain renderers stay the source of truth for piped output — every call
// site selects with out.Interactive(), and NO_COLOR/dumb terminals degrade
// through lipgloss's profile detection inside the shared tui.Theme.

// renderAligned renders a header row and pre-styled cell rows as aligned
// columns with two-space gutters. Widths are computed with lipgloss.Width, so
// cells may carry ANSI styling without breaking the alignment.
func renderAligned(st tui.Theme, headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if i < len(widths) {
				widths[i] = max(widths[i], lipgloss.Width(c))
			}
		}
	}
	pad := func(s string, col int) string {
		if col == len(widths)-1 {
			return s // last column: no trailing padding
		}
		return s + strings.Repeat(" ", widths[col]-lipgloss.Width(s)+2)
	}

	var b strings.Builder
	head := make([]string, len(headers))
	for i, h := range headers {
		head[i] = pad(h, i)
	}
	b.WriteString(st.TableHeader.Render(strings.Join(head, "")) + "\n")
	for _, row := range rows {
		for i, c := range row {
			if i >= len(widths) {
				break // cells beyond the headers have no column
			}
			b.WriteString(pad(c, i))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderListStyled renders `gskill list` for a TTY, including the
// active-layer and per-agent health columns that a separate status command
// used to show on its own (merged in spec 013; the alias was removed in
// spec 020).
func renderListStyled(skills []app.ListedSkill) string {
	if len(skills) == 0 {
		return noSkillsInstalled
	}
	st := tui.DefaultTheme()
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		rows = append(rows, []string{
			st.Accent.Render(s.Name), s.Version, st.Subtitle.Render(s.Source), st.StatusCell(s.Status),
			st.HealthCell(s.Active), agentHealthCellStyled(st, s.AgentHealth),
		})
	}
	return renderAligned(st, []string{"NAME", "VERSION", "SOURCE", "STATUS", "ACTIVE", "AGENTS"}, rows)
}

// agentHealthCellStyled renders one row's AGENTS cell: each agent as
// "id health", styled, joined by two spaces — the exact format
// `renderStatusStyled` used before the merge.
func agentHealthCellStyled(st tui.Theme, agents []app.AgentHealthEntry) string {
	cells := make([]string, 0, len(agents))
	for _, ag := range agents {
		cells = append(cells, st.Subtitle.Render(ag.ID)+" "+st.HealthCell(ag.Health))
	}
	return strings.Join(cells, "  ")
}

// renderInfoStyled renders `gskill info` for a TTY.
func renderInfoStyled(info app.SkillInfo) string {
	st := tui.DefaultTheme()
	label := func(s string) string { return st.Subtitle.Render(fmt.Sprintf("  %-8s", s)) }
	var b strings.Builder
	b.WriteString(st.Accent.Render(info.Name) + " " + st.Badge.Render("("+info.Version+")") + "\n")
	b.WriteString(label("source") + info.Source + "\n")
	b.WriteString(label("commit") + info.Commit + "\n")
	b.WriteString(label("content") + info.ContentHash + "\n")
	b.WriteString(label("desc") + info.Description + "\n")
	b.WriteString(label("agents") + strings.Join(info.Agents, ", "))
	return b.String()
}

// summary decorates a one-line success summary with the shared ✓ on
// interactive terminals; piped output passes through unchanged. The
// interactivity decision lives here, not at the call sites, so every command
// styles (and degrades) the same way.
func (o *Output) summary(text string) string {
	if !o.interactive {
		return text
	}
	return tui.DefaultTheme().Success.Render("✓ ") + text
}

// warnSummary decorates an attention summary (drift, updates pending).
func (o *Output) warnSummary(text string) string {
	if !o.interactive {
		return text
	}
	return tui.DefaultTheme().Warning.Render("◐ ") + text
}

// errSummary decorates a failure summary.
func (o *Output) errSummary(text string) string {
	if !o.interactive {
		return text
	}
	return tui.DefaultTheme().Error.Render("✗ ") + text
}

// renderFindStyled renders `gskill search` hits for a TTY.
func renderFindStyled(hits []app.SearchHit) string {
	if len(hits) == 0 {
		return "no matching skills found"
	}
	st := tui.DefaultTheme()
	rows := make([][]string, 0, len(hits))
	for _, h := range hits {
		installed := ""
		if h.Installed {
			installed = st.Success.Render("● installed")
		}
		rows = append(rows, []string{
			st.Accent.Render(h.ID), st.Subtitle.Render(h.Source), pathOrRoot(h.RepoPath), installed,
		})
	}
	return renderAligned(st, []string{"ID", "SOURCE", "PATH", ""}, rows)
}

// pathOrRoot renders an in-repo path, showing "." for the root.
func pathOrRoot(p string) string {
	if p == "" {
		return "."
	}
	return p
}

// renderSkillCatalogStyled renders discovered skills (`gskill add --list`)
// for a TTY.
func renderSkillCatalogStyled(skills []discovery.DiscoveredSkill) string {
	st := tui.DefaultTheme()
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		valid := st.Success.Render("✓ ok")
		if !s.Valid {
			valid = st.Error.Render("✗ invalid")
		}
		rows = append(rows, []string{st.Accent.Render(s.ID), valid, pathOrRoot(s.RepoPath)})
	}
	return renderAligned(st, []string{"ID", "VALID", "PATH"}, rows)
}

// renderDiffStyled renders `gskill project diff` for a TTY.
func renderDiffStyled(entries []app.DiffEntry) string {
	if len(entries) == 0 {
		return "No skills declared."
	}
	st := tui.DefaultTheme()
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			st.Accent.Render(e.Name), st.StatusCell(e.Status),
		})
	}
	return renderAligned(st, []string{"NAME", "STATUS"}, rows)
}

// renderConfigListStyled renders `gskill config list` for a TTY.
func renderConfigListStyled(values map[string]string) string {
	st := tui.DefaultTheme()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{st.Accent.Render(k), values[k]})
	}
	return renderAligned(st, []string{"KEY", "VALUE"}, rows)
}

// renderDoctorStyled renders `gskill doctor` for a TTY.
func renderDoctorStyled(report app.DoctorReport) string {
	st := tui.DefaultTheme()
	label := func(s string) string { return st.Subtitle.Render(fmt.Sprintf("%-16s", s)) }
	git := st.Error.Render("✗ false")
	if report.GitAvailable {
		git = st.Success.Render("✓ true")
	}
	warnings := strconv.Itoa(len(report.Warnings))
	if len(report.Warnings) > 0 {
		warnings = st.Warning.Render(warnings)
	}
	var b strings.Builder
	b.WriteString(label("git available:") + " " + git + "\n")
	b.WriteString(label("detected agents:") + " " + st.Accent.Render(strings.Join(report.DetectedAgents, ", ")) + "\n")
	b.WriteString(label("warnings:") + " " + warnings)
	return b.String()
}

// noSkillsDeclared is the terminal state of a project that declares nothing.
const noSkillsDeclared = "No skills declared."

// updateStatusText maps a plan status to its human table cell.
func updateStatusText(s app.UpdateStatus) string {
	switch s {
	case app.StatusUpdateAvailable:
		return "update available"
	case app.StatusUpToDate, app.StatusNoCompatibleUpdate:
		return "up to date"
	case app.StatusPinnedVersion:
		return "pinned version"
	case app.StatusPinnedTag:
		return "pinned tag"
	case app.StatusPinnedCommit:
		return "pinned commit"
	case app.StatusLocalSource:
		return "local source"
	case app.StatusLookupFailed:
		return "lookup failed"
	default:
		return string(s)
	}
}

// countUnknown counts the plan items whose eligibility could not be
// determined (discovery failure or offline skip).
func countUnknown(plan app.UpdatePlan) int {
	n := 0
	for _, it := range plan.Items {
		if it.Status == app.StatusLookupFailed {
			n++
		}
	}
	return n
}

// planCounts breaks a plan down by the reason each skill did or did not move.
type planCounts struct{ available, upToDate, pinned, local, failed int }

func countPlan(plan app.UpdatePlan) planCounts {
	var c planCounts
	for _, it := range plan.Items {
		switch {
		case it.Actionable():
			c.available++
		case it.Pinned:
			c.pinned++
		case it.Status == app.StatusLocalSource:
			c.local++
		case it.Status == app.StatusLookupFailed:
			c.failed++
		default:
			c.upToDate++
		}
	}
	return c
}

// orDash substitutes the empty-cell placeholder.
func orDash(s string) string {
	if s == "" {
		return "--"
	}
	return s
}

// renderUpdateList renders the human update report shared by
// `gskill update --list` and `gskill outdated` (spec 018 FR-006/FR-007,
// spec 024 FR-008/FR-010): candidate rows, a STATUS column plus a guidance
// line per non-actionable row under --all, and a summary that counts every
// status so an empty table is explained.
func renderUpdateList(out *Output, plan app.UpdatePlan, all bool) string {
	if len(plan.Items) == 0 {
		return noSkillsDeclared
	}
	items := plan.Actionable()
	if all {
		items = plan.Items
	}
	summary := updateListSummary(out, plan, all)
	if len(items) == 0 {
		return summary
	}

	headers := []string{"NAME", "CURRENT", "AVAILABLE", "POLICY"}
	if all {
		headers = append(headers, "STATUS")
	}
	rows := make([][]string, 0, len(items))
	details := make([]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, updateListRow(out, it, all))
		if all {
			details = append(details, updateListDetail(it))
		} else {
			details = append(details, "")
		}
	}
	return interleaveDetails(renderAligned(tui.DefaultTheme(), headers, rows), details) + "\n\n" + summary
}

// interleaveDetails places each row's guidance line directly beneath it. The
// aligned table renders one line per row after a single header line.
func interleaveDetails(table string, details []string) string {
	lines := strings.Split(table, "\n")
	if len(lines) != len(details)+1 {
		return table
	}
	var b strings.Builder
	b.WriteString(lines[0])
	for i, d := range details {
		b.WriteString("\n" + lines[i+1])
		if d != "" {
			b.WriteString("\n  " + d)
		}
	}
	return b.String()
}

// updateListDetail is the guidance line for a row a normal update cannot
// move: why, what newer exists, and the command that moves it.
func updateListDetail(it app.UpdatePlanItem) string {
	switch {
	case it.Actionable():
		if it.NextAction == "" && !strings.Contains(it.Reason, "declaration changed") {
			return ""
		}
		return it.Reason
	case it.Pinned:
		line := it.Reason
		if it.Informational != "" {
			line += "; newest " + it.Informational
		}
		return line + ". Run '" + it.NextAction + "' or edit skills.toml to move it."
	case it.Status == app.StatusNoCompatibleUpdate:
		return it.Reason + ". Run '" + it.NextAction + " --latest' to move to " + it.Informational + "."
	case it.Status == app.StatusLookupFailed:
		return it.Reason
	case it.NextAction != "":
		return it.Reason + ". Run '" + it.NextAction + "'."
	default:
		return ""
	}
}

// updateListSummary composes the report's count line (spec 024 FR-009,
// FR-010). "All skills are up to date" is claimed only when every skill was
// verified current; otherwise every status is counted, and skills hidden
// without --all are pointed at.
func updateListSummary(out *Output, plan app.UpdatePlan, all bool) string {
	c := countPlan(plan)
	if c.available == 0 && c.pinned == 0 && c.local == 0 && c.failed == 0 {
		return out.summary("All skills are up to date")
	}
	parts := []string{fmt.Sprintf("%d update%s available", c.available, plural(c.available))}
	if c.upToDate > 0 {
		parts = append(parts, fmt.Sprintf("%d up to date", c.upToDate))
	}
	if c.pinned > 0 {
		parts = append(parts, fmt.Sprintf("%d pinned", c.pinned))
	}
	if c.local > 0 {
		parts = append(parts, fmt.Sprintf("%d local", c.local))
	}
	if c.failed > 0 {
		parts = append(parts, fmt.Sprintf("%d could not be checked", c.failed))
	}
	summary := strings.Join(parts, " · ")
	if hidden := c.upToDate + c.pinned + c.local + c.failed; !all && hidden > 0 {
		summary += " (--all shows them)"
	}
	if c.available > 0 || c.failed > 0 {
		return out.warnSummary(summary)
	}
	return out.summary(summary)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// updateListRow renders one plan item's table cells.
func updateListRow(out *Output, it app.UpdatePlanItem, all bool) []string {
	name, policy, status := it.Name, it.Policy, updateStatusText(it.Status)
	switch {
	case it.Status == app.StatusNoCompatibleUpdate && it.Informational != "":
		// A newer release the policy forbids is named right in the status
		// cell (FR-007) — AVAILABLE stays `--` because a normal update never
		// applies it.
		status += " (newer " + it.Informational + " outside " + it.Policy + ")"
	case it.Status == app.StatusLookupFailed && it.DiscoveryErr == "":
		status += " (offline)"
	}
	if out.Interactive() {
		st := tui.DefaultTheme()
		name = st.Accent.Render(name)
		policy = st.Subtitle.Render(policy)
		if it.Status == app.StatusUpdateAvailable {
			status = st.Warning.Render(status)
		} else {
			status = st.Subtitle.Render(status)
		}
	}
	row := []string{name, it.Current, orDash(it.Candidate), policy}
	if all {
		row = append(row, status)
	}
	return row
}

// updateOutcomeText maps one execution row to its RESULT cell.
func updateOutcomeText(s app.UpdateSkillResult) string {
	switch s.Outcome {
	case app.UpdateOutcomeUpdated:
		return "updated"
	case app.UpdateOutcomeWould:
		return "would update"
	case app.UpdateOutcomeFailed:
		return "failed"
	case app.UpdateOutcomeNoChange:
		if s.Status != "" {
			return updateStatusText(s.Status)
		}
		return "no change"
	default:
		return "no change"
	}
}

// renderUpdateResult renders an update execution: per-skill NAME/FROM/TO/
// RESULT rows plus a run summary (spec 018 FR-015) — never a bare content
// hash.
func renderUpdateResult(out *Output, res app.UpdateResult) string {
	summary := updateSummary(res)
	if len(res.Skills) == 0 {
		return out.summary(summary)
	}

	st := tui.DefaultTheme()
	rows := make([][]string, 0, len(res.Skills))
	for _, s := range res.Skills {
		rows = append(rows, updateResultRow(st, out.Interactive(), s))
	}
	table := renderAligned(st, []string{"NAME", "FROM", "TO", "RESULT"}, rows)
	if details := updateResultDetails(res); len(details) > 0 {
		table += "\n\n" + strings.Join(details, "\n")
	}

	if res.Failed > 0 {
		summary = out.errSummary(summary)
	} else {
		summary = out.summary(summary)
	}
	rendered := table + "\n\n" + summary
	if res.DryRun {
		rendered += "\nDry run: no changes made."
	}
	return rendered
}

// updateResultRow renders one execution row, styled for a TTY.
func updateResultRow(st tui.Theme, styled bool, s app.UpdateSkillResult) []string {
	name, result := s.Name, updateOutcomeText(s)
	if styled {
		name = st.Accent.Render(name)
		switch s.Outcome {
		case app.UpdateOutcomeFailed:
			result = st.Error.Render(result)
		case app.UpdateOutcomeUpdated, app.UpdateOutcomeWould:
			result = st.Success.Render(result)
		case app.UpdateOutcomeNoChange:
			result = st.Subtitle.Render(result)
		default:
			result = st.Subtitle.Render(result)
		}
	}
	return []string{name, s.From, s.To, result}
}

// updateResultDetails lists the lines beneath the table: a failed row's
// reason (a bare "failed" cell would force the user into --json to learn
// why), and for a pinned row the declaration that pins it and the command
// that moves it (spec 024 FR-008).
func updateResultDetails(res app.UpdateResult) []string {
	var details []string
	for _, s := range res.Skills {
		switch {
		case s.Outcome == app.UpdateOutcomeFailed && s.Reason != "":
			details = append(details, "  "+s.Name+": "+s.Reason)
		case s.Outcome == app.UpdateOutcomeNoChange && s.Status.Pinned():
			line := "  " + s.Name + ": " + s.Reason
			if s.Informational != "" {
				line += "; newest " + s.Informational
			}
			details = append(details, line+". Run '"+s.NextAction+"' or edit skills.toml to move it.")
		case s.Outcome == app.UpdateOutcomeNoChange && s.Status == app.StatusNoCompatibleUpdate && s.NextAction != "":
			details = append(details, "  "+s.Name+": "+s.Reason+". Run '"+s.NextAction+" --latest' to move to "+s.Informational+".")
		}
	}
	return details
}

// renderPlanTextStyled renders the `add --dry-run` plan for a TTY: the exact
// text of renderPlanText with the wizard preview's per-kind colors, so the
// two plan surfaces read identically (FR-015/FR-024).
func renderPlanTextStyled(plan app.InstallPlan) string {
	st := tui.DefaultTheme()
	var b strings.Builder
	b.WriteString(st.Title.Render("Plan (dry run — nothing will be written):") + "\n")
	for _, pl := range plan.Lines("") {
		switch pl.Kind {
		case app.PlanLineAction:
			fmt.Fprintf(&b, "  + %s\n", pl.Text)
		case app.PlanLineFileOp:
			fmt.Fprintf(&b, "      %s\n", st.Hint.Render(pl.Text))
		case app.PlanLineWarning:
			fmt.Fprintf(&b, "  %s\n", st.Warning.Render("warning: "+pl.Text))
		case app.PlanLineConflict:
			fmt.Fprintf(&b, "  %s\n", st.Error.Render("conflict: "+pl.Text))
		case app.PlanLineMeta:
			fmt.Fprintf(&b, "  %s\n", st.Accent.Render(pl.Text))
		case app.PlanLineInit:
			fmt.Fprintf(&b, "  %s\n", st.Warning.Render(pl.Text))
		case app.PlanLineAgent:
			fmt.Fprintf(&b, "  %s\n", st.Subtitle.Render(pl.Text))
		default:
			fmt.Fprintf(&b, "  %s\n", pl.Text)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
