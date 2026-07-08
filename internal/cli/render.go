package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/glapsfun/gskill/internal/app"
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
			b.WriteString(pad(c, i))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderListStyled renders `gskill list` for a TTY.
func renderListStyled(skills []app.ListedSkill) string {
	if len(skills) == 0 {
		return "No skills installed."
	}
	st := tui.DefaultTheme()
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		rows = append(rows, []string{
			st.Accent.Render(s.Name), s.Version, st.Subtitle.Render(s.Source), st.StatusCell(s.Status),
		})
	}
	return renderAligned(st, []string{"NAME", "VERSION", "SOURCE", "STATUS"}, rows)
}

// renderStatusStyled renders `gskill status` for a TTY.
func renderStatusStyled(report app.StatusReport) string {
	if len(report.Skills) == 0 {
		return "0 skill(s)"
	}
	st := tui.DefaultTheme()
	rows := make([][]string, 0, len(report.Skills))
	for _, s := range report.Skills {
		agents := make([]string, 0, len(s.Agents))
		for _, ag := range s.Agents {
			agents = append(agents, st.Subtitle.Render(ag.ID)+" "+st.HealthCell(ag.Health))
		}
		rows = append(rows, []string{
			st.Accent.Render(s.Name), st.HealthCell(s.Active), strings.Join(agents, "  "),
		})
	}
	return renderAligned(st, []string{"NAME", "ACTIVE", "AGENTS"}, rows)
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

// styledSummary decorates a one-line success summary with the shared ✓.
func styledSummary(text string) string {
	return tui.DefaultTheme().Success.Render("✓ ") + text
}
