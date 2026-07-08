package cli

import (
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/tui"
)

// Styled human output for interactive terminals (design 2026-07-08):
// kubectl-style aligned columns, semantic status glyphs, no borders. The
// plain renderers stay the source of truth for piped output — every call
// site selects with out.Interactive(), and NO_COLOR/dumb terminals degrade
// through lipgloss's profile detection inside the shared tui.Theme.

// renderListStyled renders `gskill list` for a TTY.
func renderListStyled(skills []app.ListedSkill) string {
	if len(skills) == 0 {
		return "No skills installed."
	}
	st := tui.DefaultTheme()
	nameW, verW, srcW := len("NAME"), len("VERSION"), len("SOURCE")
	for _, s := range skills {
		nameW, verW, srcW = max(nameW, len(s.Name)), max(verW, len(s.Version)), max(srcW, len(s.Source))
	}
	pad := func(s string, w int) string { return s + strings.Repeat(" ", w-len(s)+2) }

	var b strings.Builder
	b.WriteString(st.TableHeader.Render(pad("NAME", nameW)+pad("VERSION", verW)+pad("SOURCE", srcW)+"STATUS") + "\n")
	for _, s := range skills {
		b.WriteString(st.Accent.Render(pad(s.Name, nameW)))
		b.WriteString(pad(s.Version, verW))
		b.WriteString(st.Subtitle.Render(pad(s.Source, srcW)))
		b.WriteString(st.StatusCell(s.Status))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderStatusStyled renders `gskill status` for a TTY.
func renderStatusStyled(report app.StatusReport) string {
	if len(report.Skills) == 0 {
		return "0 skill(s)"
	}
	st := tui.DefaultTheme()
	nameW, activeW := len("NAME"), len("ACTIVE")
	for _, s := range report.Skills {
		nameW, activeW = max(nameW, len(s.Name)), max(activeW, len(s.Active)+2)
	}
	pad := func(s string, w int) string { return s + strings.Repeat(" ", w-len(s)+2) }

	var b strings.Builder
	b.WriteString(st.TableHeader.Render(pad("NAME", nameW)+pad("ACTIVE", activeW)+"AGENTS") + "\n")
	for _, s := range report.Skills {
		b.WriteString(st.Accent.Render(pad(s.Name, nameW)))
		// The glyph adds two visible runes; pad the raw state to keep columns.
		b.WriteString(st.HealthCell(s.Active) + strings.Repeat(" ", activeW-len(s.Active)))
		agents := make([]string, 0, len(s.Agents))
		for _, ag := range s.Agents {
			agents = append(agents, st.Subtitle.Render(ag.ID)+" "+st.HealthCell(ag.Health))
		}
		b.WriteString(strings.Join(agents, "  "))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
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
