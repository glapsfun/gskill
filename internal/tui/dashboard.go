package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/glapsfun/gskill/internal/errs"
)

// SkillRow is one entry shown in the dashboard.
type SkillRow struct {
	Name     string
	Version  string
	Source   string
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

// previewMinHeight is the terminal height below which the preview strip
// collapses so the table keeps a usable page (design: 20-row floor).
const previewMinHeight = 20

// model is the dashboard state: a themed skills table stacked over a bordered
// preview viewport showing the cursor row's SKILL.md (design 2026-07-08).
type model struct {
	rows         []SkillRow
	st           Theme
	tbl          table.Model
	vp           viewport.Model
	focusPreview bool

	// previewCache memoizes rendered SKILL.md previews per row index for the
	// current viewport width (cleared on resize): glamour builds a fresh
	// renderer per call, which is too slow to repeat on every cursor move.
	previewCache map[int]string

	width, height int
}

// newModel builds the dashboard model.
func newModel(rows []SkillRow) model {
	st := DefaultTheme()
	tbl := table.New(
		table.WithColumns(dashColumns(defaultWidth)),
		table.WithRows(dashRowsData(rows, st)),
		table.WithFocused(true),
	)
	tbl.SetStyles(st.TableStyles())
	m := model{
		rows: rows, st: st, tbl: tbl, vp: viewport.New(defaultWidth, 6),
		previewCache: make(map[int]string), width: defaultWidth,
	}
	m.refreshPreview()
	return m
}

// dashColumns sizes the table columns for a terminal width: VERSION and
// STATUS are fixed, NAME and SOURCE flex. pad reserves the per-column cell
// padding bubbles renders around each of the four columns (2 × 4) plus a
// right margin (6).
func dashColumns(width int) []table.Column {
	const version, status, pad = 10, 24, 14
	flex := max(20, width-version-status-pad)
	return []table.Column{
		{Title: "NAME", Width: flex / 2},
		{Title: "VERSION", Width: version},
		{Title: "SOURCE", Width: flex - flex/2},
		{Title: "STATUS", Width: status},
	}
}

// dashRowsData renders SkillRows as table rows with sanitized cells. Cells
// must stay free of ANSI sequences: bubbles/table truncates every cell with a
// width function that counts escape bytes as visible columns, so a styled
// cell gets cut mid-sequence and its color bleeds into the rest of the table.
// StatusCell supplies the glyph vocabulary; Strip removes its styling.
func dashRowsData(rows []SkillRow, st Theme) []table.Row {
	out := make([]table.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, table.Row{
			Sanitize(r.Name), Sanitize(r.Version), Sanitize(r.Source),
			ansi.Strip(st.StatusCell(Sanitize(r.Status))),
		})
	}
	return out
}

// showPreview reports whether the preview strip fits this terminal.
func (m model) showPreview() bool {
	return m.height >= previewMinHeight && len(m.rows) > 0
}

// layout recomputes region sizes from the terminal size.
func (m *model) layout() {
	m.tbl.SetColumns(dashColumns(m.width))
	m.tbl.SetWidth(m.width)
	previewH := 0
	if m.showPreview() {
		previewH = m.height / 3
	} else {
		// The preview strip is gone; keys must go back to the table, or
		// navigation would scroll an invisible viewport.
		m.focusPreview = false
	}
	// Frame: header (2 lines) + footer (1 line) + the preview's title line and
	// panel border (3 lines when shown).
	tableH := m.height - previewH - 3
	if m.showPreview() {
		tableH -= 3
	}
	m.tbl.SetHeight(max(3, tableH))
	m.vp.Width = m.width - 4
	if previewH > 3 {
		m.vp.Height = previewH - 3
	} else {
		m.vp.Height = 1
	}
	clear(m.previewCache) // renders are width-specific
	m.refreshPreview()
}

// refreshPreview re-renders the cursor row's SKILL.md into the viewport.
func (m *model) refreshPreview() {
	if len(m.rows) == 0 {
		return
	}
	i := m.tbl.Cursor()
	if i < 0 || i >= len(m.rows) {
		i = 0
	}
	body, ok := m.previewCache[i]
	if !ok {
		var err error
		if body, err = Preview(m.rows[i].Markdown, m.vp.Width); err != nil {
			body = Sanitize(m.rows[i].Markdown)
		}
		m.previewCache[i] = body
	}
	m.vp.SetContent(body)
	m.vp.GotoTop()
}

// Init implements tea.Model.
func (m model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", keyCtrlC, keyEsc:
			return m, tea.Quit
		case "tab":
			m.focusPreview = !m.focusPreview && m.showPreview()
			return m, nil
		}
		if m.focusPreview {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
		before := m.tbl.Cursor()
		var cmd tea.Cmd
		m.tbl, cmd = m.tbl.Update(msg)
		if m.tbl.Cursor() != before {
			m.refreshPreview()
		}
		return m, cmd
	}
	return m, nil
}

// View implements tea.Model.
func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.st.Accent.Render(" gskill") + m.st.Subtitle.Render(" · installed skills") + "\n\n")

	if len(m.rows) == 0 {
		b.WriteString("No skills installed.\n\n")
		b.WriteString(m.st.Subtitle.Render("Run 'gskill onboard' to install your first skill.") + "\n")
		b.WriteString(m.st.Hint.Render("q quit") + "\n")
		return b.String()
	}

	b.WriteString(m.tbl.View() + "\n")

	if m.showPreview() {
		i := m.tbl.Cursor()
		title := fmt.Sprintf("%s %s", Sanitize(m.rows[i].Name), Sanitize(m.rows[i].Version))
		b.WriteString(m.st.Accent.Render(" "+title) + "\n")
		panel := m.st.Panel().Width(max(20, m.width-2))
		b.WriteString(panel.Render(m.vp.View()) + "\n")
	}

	pos := fmt.Sprintf("%d skills · %d/%d", len(m.rows), m.tbl.Cursor()+1, len(m.rows))
	b.WriteString(m.st.Hint.Render(" ↑/↓ move · tab focus · q quit") + "  " + m.st.Badge.Render(pos) + "\n")
	return b.String()
}
