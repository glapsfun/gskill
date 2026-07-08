package tui

import "github.com/charmbracelet/lipgloss"

// wizardStyles is the wizard's visual vocabulary (spec 011 FR-022). Every
// color is a lipgloss.AdaptiveColor so light and dark terminals both read
// well, and lipgloss's automatic color-profile detection (via termenv)
// degrades to reduced palettes or plain text on NO_COLOR/dumb terminals —
// no capability probing of our own.
type wizardStyles struct {
	Title    lipgloss.Style // step headings
	Subtitle lipgloss.Style // secondary heading line
	Accent   lipgloss.Style // source names, counts, selections
	Hint     lipgloss.Style // the keyboard hint footer
	Cursor   lipgloss.Style // the "❯" row cursor
	Selected lipgloss.Style // chosen items / active option
	Invalid  lipgloss.Style // non-selectable rows
	Success  lipgloss.Style // summary confirmation lines
	Warning  lipgloss.Style // degraded notes, warnings
	Error    lipgloss.Style // failures, conflicts
	Badge    lipgloss.Style // step-position badge ("step 2/6")
}

func defaultWizardStyles() wizardStyles {
	var (
		accent  = lipgloss.AdaptiveColor{Light: "#5B4FE9", Dark: "#8B83FF"}
		subtle  = lipgloss.AdaptiveColor{Light: "#6C6F85", Dark: "#9399B2"}
		good    = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
		warning = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
		bad     = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	)
	return wizardStyles{
		Title:    lipgloss.NewStyle().Bold(true),
		Subtitle: lipgloss.NewStyle().Foreground(subtle),
		Accent:   lipgloss.NewStyle().Foreground(accent).Bold(true),
		Hint:     lipgloss.NewStyle().Foreground(subtle),
		Cursor:   lipgloss.NewStyle().Foreground(accent).Bold(true),
		Selected: lipgloss.NewStyle().Foreground(accent),
		Invalid:  lipgloss.NewStyle().Foreground(subtle).Strikethrough(true),
		Success:  lipgloss.NewStyle().Foreground(good),
		Warning:  lipgloss.NewStyle().Foreground(warning),
		Error:    lipgloss.NewStyle().Foreground(bad).Bold(true),
		Badge:    lipgloss.NewStyle().Foreground(subtle),
	}
}
