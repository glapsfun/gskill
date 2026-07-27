package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/glapsfun/gskill/internal/errs"
)

// UpdateItem is one actionable update candidate in the interactive update
// selector (spec 018 US3). All items offered for selection are actionable by
// construction — pins and up-to-date skills never reach the selector.
type UpdateItem struct {
	Name      string
	Current   string
	Candidate string
	Policy    string
}

// UpdateSelection is the outcome of the update selector. Cancelled (esc/q)
// and Interrupted (ctrl-c) both mean "apply nothing"; the CLI maps the former
// to a friendly exit 0 and the latter to the interrupt exit code.
type UpdateSelection struct {
	Names       []string
	Cancelled   bool
	Interrupted bool
}

// SelectUpdates runs the interactive multi-select over actionable updates and
// returns the confirmed names. It refuses to start without a TTY: the CLI's
// interactivity gate decides when a selector may open, never this package.
func SelectUpdates(items []UpdateItem, isTTY bool) (UpdateSelection, error) {
	if !isTTY {
		return UpdateSelection{}, fmt.Errorf("%w: interactive update selection requires a TTY; pass skill names or --no-interactive", errs.ErrUsage)
	}
	final, err := tea.NewProgram(newUpdateSelectorModel(items)).Run()
	if err != nil {
		return UpdateSelection{}, fmt.Errorf("tui: %w", err)
	}
	m, ok := final.(updateSelectorModel)
	if !ok {
		return UpdateSelection{Cancelled: true}, nil
	}
	return m.selection(), nil
}

// updateSelectorModel is the update multi-select state: selectorModel's
// bounded-window/filter/original-index structure with version-transition
// columns instead of skill metadata (research D7).
type updateSelectorModel struct {
	items       []UpdateItem
	chosen      map[int]bool // keyed by original index
	cursor      int          // index into visible
	offset      int          // index into visible of the first on-screen row
	height      int
	width       int
	filtering   bool
	filter      lineInput
	visible     []int // original indices matching the filter, in order
	st          Theme
	nameW       int // aligned name column width over the visible set
	currentW    int // aligned current-revision column width
	candidateW  int // aligned candidate-revision column width
	done        bool
	cancelled   bool
	interrupted bool
}

func newUpdateSelectorModel(items []UpdateItem) updateSelectorModel {
	m := updateSelectorModel{items: items, chosen: make(map[int]bool), st: DefaultTheme()}
	m.recomputeVisible()
	return m
}

// Init implements tea.Model.
func (m updateSelectorModel) Init() tea.Cmd { return nil }

// pageSize returns the number of rows that fit on screen, never below 1.
func (m updateSelectorModel) pageSize() int {
	return pageFor(m.height, reservedRows)
}

// recomputeVisible rebuilds the filtered index list (prefix-first over names,
// substring fallback) and the aligned column widths, then re-clamps.
func (m *updateSelectorModel) recomputeVisible() {
	q := strings.ToLower(strings.TrimSpace(m.filter.value))
	m.visible = m.visible[:0]
	if q != "" {
		for i, it := range m.items {
			if strings.HasPrefix(strings.ToLower(it.Name), q) {
				m.visible = append(m.visible, i)
			}
		}
	}
	if len(m.visible) == 0 {
		for i, it := range m.items {
			if q == "" || strings.Contains(strings.ToLower(it.Name), q) {
				m.visible = append(m.visible, i)
			}
		}
	}
	m.nameW, m.currentW, m.candidateW = 0, 0, 0
	for _, i := range m.visible {
		it := m.items[i]
		m.nameW = max(m.nameW, ansi.StringWidth(it.Name))
		m.currentW = max(m.currentW, ansi.StringWidth(it.Current))
		m.candidateW = max(m.candidateW, ansi.StringWidth(it.Candidate))
	}
	m.nameW = min(m.nameW, nameColMax)
	m.clamp()
}

// clamp keeps the cursor in range and the scroll offset on screen.
func (m *updateSelectorModel) clamp() {
	if len(m.visible) == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	if m.cursor > len(m.visible)-1 {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	ps := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+ps {
		m.offset = m.cursor - ps + 1
	}
	if maxOffset := len(m.visible) - ps; m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// toggle flips the selection of the row under the cursor.
func (m *updateSelectorModel) toggle() {
	if len(m.visible) == 0 {
		return
	}
	orig := m.visible[m.cursor]
	m.chosen[orig] = !m.chosen[orig]
}

// Update implements tea.Model.
func (m updateSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height, m.width = msg.Height, msg.Width
		m.clamp()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// moveUp moves the cursor up one visible row.
func (m *updateSelectorModel) moveUp() {
	if m.cursor > 0 {
		m.cursor--
		m.clamp()
	}
}

// moveDown moves the cursor down one visible row.
func (m *updateSelectorModel) moveDown() {
	if m.cursor < len(m.visible)-1 {
		m.cursor++
		m.clamp()
	}
}

// handleKey mirrors the install selector's contract: space always toggles,
// enter confirms, esc unfocuses/clears the filter before cancelling, ctrl-c
// interrupts.
func (m updateSelectorModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case keyCtrlC:
		m.interrupted = true
		return m, tea.Quit
	case keyEnter:
		m.done = true
		return m, tea.Quit
	case keyUp:
		m.moveUp()
		return m, nil
	case keyDown:
		m.moveDown()
		return m, nil
	case keyEsc:
		return m.handleEsc()
	}
	if m.filtering {
		return m.handleFilterKey(key)
	}
	return m.handleNavKey(key)
}

// handleEsc is two-stage: unfocus an active filter input, then clear an
// applied query, then cancel.
func (m updateSelectorModel) handleEsc() (tea.Model, tea.Cmd) {
	if m.filtering {
		m.filtering = false
		return m, nil
	}
	if m.filter.value != "" {
		m.filter.value = ""
		m.recomputeVisible()
		return m, nil
	}
	m.cancelled = true
	return m, tea.Quit
}

// handleFilterKey edits the filter query; space still toggles the highlighted
// row so multi-select works mid-search.
func (m updateSelectorModel) handleFilterKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == " " {
		m.toggle()
		return m, nil
	}
	if m.filter.handleKey(key) {
		m.recomputeVisible()
	}
	return m, nil
}

// handleNavKey handles navigation shortcuts when the filter is not focused.
func (m updateSelectorModel) handleNavKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "k":
		m.moveUp()
	case "j":
		m.moveDown()
	case " ", "x":
		m.toggle()
	case "/":
		m.filtering = true
	case "q":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// fit truncates one rendered line to the terminal width (ANSI-aware), so a
// narrow terminal never soft-wraps the frame (spec 018 TUI test plan).
func (m updateSelectorModel) fit(s string) string {
	if m.width <= 0 {
		return s
	}
	return ansi.Truncate(s, m.width, "…")
}

// View implements tea.Model.
func (m updateSelectorModel) View() string {
	var b strings.Builder
	b.WriteString(m.fit(m.st.Title.Render("Select updates (space toggles, enter confirms):")))
	b.WriteByte('\n')
	if m.filtering || m.filter.value != "" {
		b.WriteString(m.fit(fmt.Sprintf("%s %s", m.st.Accent.Render("Filter:"), m.filter.value)))
		b.WriteByte('\n')
	} else {
		b.WriteByte('\n')
	}

	b.WriteString(m.viewRows())

	b.WriteByte('\n')
	b.WriteString(m.fit(fmt.Sprintf("%s  •  %s",
		m.st.Badge.Render(m.position()),
		m.st.Hint.Render("↑/↓ move · space toggle · / filter · enter confirm · esc cancel"))))
	b.WriteByte('\n')
	return b.String()
}

// viewRows renders the bounded, scrolling row window — or the terminal-state
// message when there is nothing to show.
func (m updateSelectorModel) viewRows() string {
	if len(m.visible) == 0 {
		if len(m.items) == 0 {
			return "  (no updates available — all skills are up to date)\n"
		}
		return "  (no matches)\n"
	}
	var b strings.Builder
	start, end, above, below := windowBounds(len(m.visible), m.pageSize(), m.offset)
	if above {
		b.WriteString("  ↑ more\n")
	}
	for vi := start; vi < end; vi++ {
		b.WriteString(m.rowString(vi))
		b.WriteByte('\n')
	}
	if below {
		b.WriteString("  ↓ more\n")
	}
	return b.String()
}

// position renders cursor position, filtered counts, and selection count.
func (m updateSelectorModel) position() string {
	pos := fmt.Sprintf("%d/%d", min(m.cursor+1, len(m.visible)), len(m.visible))
	if len(m.visible) != len(m.items) {
		pos += fmt.Sprintf(" (of %d)", len(m.items))
	}
	if n := len(m.selection().Names); n > 0 {
		pos += fmt.Sprintf(" · %d selected", n)
	}
	return pos
}

// rowString renders one aligned row: checkbox, name, current -> candidate,
// and tracking policy, truncated ANSI-aware to the terminal width.
func (m updateSelectorModel) rowString(vi int) string {
	orig := m.visible[vi]
	it := m.items[orig]

	cursor := "  "
	if vi == m.cursor {
		cursor = m.st.Cursor.Render("❯") + " "
	}
	check := "[ ] "
	if m.chosen[orig] {
		check = m.st.Success.Render("[✓]") + " "
	}

	name := ansi.Truncate(it.Name, m.nameW, "…")
	if pad := m.nameW - ansi.StringWidth(name); pad > 0 {
		name += strings.Repeat(" ", pad)
	}
	if vi == m.cursor {
		name = m.st.Selected.Render(name)
	}
	current := it.Current + strings.Repeat(" ", m.currentW-ansi.StringWidth(it.Current))
	candidate := it.Candidate + strings.Repeat(" ", m.candidateW-ansi.StringWidth(it.Candidate))

	row := cursor + check + name + "  " + current + " -> " + candidate + "  " + m.st.Subtitle.Render(it.Policy)
	if m.width <= 0 {
		return row
	}
	return ansi.Truncate(row, m.width, "…")
}

// selection returns the confirmed names (original order) and cancel flags.
func (m updateSelectorModel) selection() UpdateSelection {
	sel := UpdateSelection{Cancelled: m.cancelled, Interrupted: m.interrupted}
	if sel.Cancelled || sel.Interrupted {
		return sel
	}
	for i, it := range m.items {
		if m.chosen[i] {
			sel.Names = append(sel.Names, it.Name)
		}
	}
	return sel
}
