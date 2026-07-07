package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/errs"
)

// SkillRow is one entry shown in the dashboard.
type SkillRow struct {
	Name     string
	Status   string
	Markdown string // SKILL.md content for the preview pane
}

// Run launches the interactive dashboard. It refuses to start without a TTY,
// returning a usage error that points at the equivalent CLI commands (FR-041).
func Run(rows []SkillRow, isTTY bool) error {
	if !isTTY {
		return fmt.Errorf("%w: the TUI requires a TTY; use 'gskill list' or 'gskill info <name>' instead",
			errs.ErrUsage)
	}
	if _, err := tea.NewProgram(newModel(rows), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// model is the dashboard state.
type model struct {
	rows   []SkillRow
	cursor int
	width  int
}

// newModel builds the dashboard model.
func newModel(rows []SkillRow) model {
	return model{rows: rows, width: defaultWidth}
}

// Init implements tea.Model.
func (m model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "q", keyCtrlC, keyEsc:
			return m, tea.Quit
		case keyUp, "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case keyDown, "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m model) View() string {
	var b strings.Builder
	b.WriteString("gskill — installed skills\n\n")
	if len(m.rows) == 0 {
		b.WriteString("No skills installed.\n\nPress q to quit.\n")
		return b.String()
	}

	for i, r := range m.rows {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		_, _ = fmt.Fprintf(&b, "%s%s  [%s]\n", marker, r.Name, r.Status)
	}
	b.WriteString("\n")

	if preview, err := Preview(m.rows[m.cursor].Markdown, m.width-4); err == nil {
		b.WriteString(preview)
	}
	b.WriteString("\n(↑/↓ to move · q to quit)\n")
	return b.String()
}
