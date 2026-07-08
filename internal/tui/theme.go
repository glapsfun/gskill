package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Theme is the package-wide visual vocabulary (design 2026-07-08). Every color
// is a lipgloss.AdaptiveColor so light and dark terminals both read well, and
// lipgloss's automatic color-profile detection (via termenv) degrades to
// reduced palettes or plain text on NO_COLOR/dumb terminals — no capability
// probing of our own. One Theme feeds every surface: the wizard (directly and
// via Huh()), the dashboard (via TableStyles()), and the CLI's styled output,
// so the surfaces cannot drift apart.
type Theme struct {
	Title    lipgloss.Style // step/screen headings
	Subtitle lipgloss.Style // secondary heading line
	Accent   lipgloss.Style // source names, counts, selections
	Hint     lipgloss.Style // keyboard hint footers
	Cursor   lipgloss.Style // the "❯" row cursor
	Selected lipgloss.Style // chosen items / active option
	Invalid  lipgloss.Style // non-selectable rows
	Success  lipgloss.Style // confirmation lines
	Warning  lipgloss.Style // degraded notes, warnings
	Error    lipgloss.Style // failures, conflicts
	Badge    lipgloss.Style // step-position badge ("step 2/6")

	Border      lipgloss.Style // panel borders
	TableHeader lipgloss.Style // column header rows
}

// DefaultTheme returns the indigo-minimal palette.
func DefaultTheme() Theme {
	var (
		accent  = lipgloss.AdaptiveColor{Light: "#5B4FE9", Dark: "#8B83FF"}
		subtle  = lipgloss.AdaptiveColor{Light: "#6C6F85", Dark: "#9399B2"}
		good    = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
		warning = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
		bad     = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
		border  = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	)
	return Theme{
		Title:       lipgloss.NewStyle().Bold(true),
		Subtitle:    lipgloss.NewStyle().Foreground(subtle),
		Accent:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		Hint:        lipgloss.NewStyle().Foreground(subtle),
		Cursor:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		Selected:    lipgloss.NewStyle().Foreground(accent),
		Invalid:     lipgloss.NewStyle().Foreground(subtle).Strikethrough(true),
		Success:     lipgloss.NewStyle().Foreground(good),
		Warning:     lipgloss.NewStyle().Foreground(warning),
		Error:       lipgloss.NewStyle().Foreground(bad).Bold(true),
		Badge:       lipgloss.NewStyle().Foreground(subtle),
		Border:      lipgloss.NewStyle().Foreground(border),
		TableHeader: lipgloss.NewStyle().Foreground(subtle).Bold(true),
	}
}

// StatusCell renders a drift status with its semantic glyph and color
// (internal/integrity/driftstatus.go vocabulary). Unknown statuses render
// plain so a new status is never hidden or miscolored.
func (t Theme) StatusCell(status string) string {
	switch status {
	case "installed":
		return t.Success.Render("● " + status)
	case "modified", "outdated", "partially-installed":
		return t.Warning.Render("◐ " + status)
	case "missing", "orphaned", "source-unavailable",
		"checksum-mismatch", "manifest-lock-mismatch":
		return t.Error.Render("✗ " + status)
	default:
		return status
	}
}

// Panel returns a thin bordered panel style in the theme's border color.
func (t Theme) Panel() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.Border.GetForeground())
}

// TableStyles adapts the theme for bubbles/table: subtle bold headers over a
// thin rule and a foreground-accent (not background-highlight) cursor row,
// per the indigo-minimal design.
func (t Theme) TableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(t.TableHeader.GetForeground()).
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Border.GetForeground()).
		BorderBottom(true)
	s.Selected = lipgloss.NewStyle().Foreground(t.Accent.GetForeground()).Bold(true)
	s.Cell = lipgloss.NewStyle()
	return s
}

// Huh adapts the theme for huh forms (the agents and version steps), mapping
// the wizard vocabulary onto huh's FieldStyles.
func (t Theme) Huh() *huh.Theme {
	h := huh.ThemeBase()
	h.Focused.Title = t.Title
	h.Focused.Description = t.Subtitle
	h.Focused.ErrorMessage = t.Error
	h.Focused.ErrorIndicator = t.Error
	h.Focused.SelectSelector = t.Cursor.SetString("❯ ")
	h.Focused.MultiSelectSelector = t.Cursor.SetString("❯ ")
	h.Focused.SelectedOption = t.Selected
	h.Focused.SelectedPrefix = t.Success.SetString("[✓] ")
	h.Focused.UnselectedPrefix = lipgloss.NewStyle().SetString("[ ] ")
	h.Focused.UnselectedOption = lipgloss.NewStyle()
	h.Focused.TextInput.Prompt = t.Accent
	h.Focused.TextInput.Placeholder = t.Hint
	h.Blurred = h.Focused
	return h
}
