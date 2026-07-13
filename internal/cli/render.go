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
// active-layer and per-agent health columns that `gskill status` used to
// show on its own (spec 013).
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

// renderSkillCatalogStyled renders discovered skills (`gskill source list`,
// `gskill add --list`) for a TTY.
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
