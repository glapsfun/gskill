package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/glapsfun/gskill/internal/errs"
)

const (
	// reservedRows is the number of non-list lines the picker frames around the
	// scrollable window: the header, the filter line, the more-above and
	// more-below markers, a blank spacer, and the position/help footer (6 in
	// total). The visible page size is the terminal height minus these rows.
	reservedRows = 6
	// defaultPageSize bounds the list before the first WindowSizeMsg arrives, so
	// a large list never renders unbounded even if a size is never reported.
	defaultPageSize = 10
)

// SkillItem is one selectable skill in the interactive picker.
type SkillItem struct {
	ID          string
	DisplayName string
	Description string
	RepoPath    string
	Valid       bool
	// InvalidReason is the first error-severity diagnostic for an invalid
	// skill, shown when the cursor is on its row (FR-011).
	InvalidReason string
}

// SelectSkills runs the interactive multi-select picker and returns the indices
// of the chosen skills. It refuses to start without a TTY. A cancelled picker
// returns an empty selection and a nil error (the caller treats that as "no
// skill selected").
func SelectSkills(items []SkillItem, isTTY bool) ([]int, error) {
	if !isTTY {
		return nil, fmt.Errorf("%w: interactive selection requires a TTY; pass --skill, --all, or --list", errs.ErrUsage)
	}
	final, err := tea.NewProgram(newSelectorModel(items)).Run()
	if err != nil {
		return nil, fmt.Errorf("tui: %w", err)
	}
	m, ok := final.(selectorModel)
	if !ok || m.cancelled {
		return nil, nil
	}
	return m.chosenIndices(), nil
}

// selectorModel is the multi-select picker state. The list is rendered through
// a bounded, scrolling viewport and can be narrowed with a substring filter, so
// the picker stays usable for repositories that discover many skills.
//
// Selections (chosen) are keyed by the item's original index, so toggles
// survive scrolling and filtering. cursor and offset index into visible, the
// filtered list of original indices.
type selectorModel struct {
	items     []SkillItem
	chosen    map[int]bool
	cursor    int  // index into visible
	offset    int  // index into visible of the first on-screen row
	height    int  // last known terminal height (from WindowSizeMsg)
	width     int  // last known terminal width
	filtering bool // whether the filter input is focused
	filter    lineInput
	visible   []int // original indices matching query, in order
	reserved  int   // frame rows around the row window (embedders override)
	st        Theme
	nameW     int // aligned name-column width, recomputed with the visible set
	done      bool
	cancelled bool
}

func newSelectorModel(items []SkillItem) selectorModel {
	m := selectorModel{items: items, chosen: make(map[int]bool), reserved: reservedRows, st: DefaultTheme()}
	m.recomputeVisible()
	return m
}

// Init implements tea.Model.
func (m selectorModel) Init() tea.Cmd { return nil }

// pageSize returns the number of skill rows that fit on screen, never below 1.
func (m selectorModel) pageSize() int {
	return pageFor(m.height, m.reserved)
}

// recomputeVisible rebuilds the filtered index list from the current query
// (case-insensitive substring match over ID, RepoPath, and Description —
// FR-010) and re-clamps the cursor and scroll offset. It never touches the
// selection set.
func (m *selectorModel) recomputeVisible() {
	q := strings.ToLower(strings.TrimSpace(m.filter.value))
	m.visible = m.visible[:0]
	for i, it := range m.items {
		if q == "" ||
			strings.Contains(strings.ToLower(it.ID), q) ||
			strings.Contains(strings.ToLower(it.RepoPath), q) ||
			strings.Contains(strings.ToLower(it.Description), q) {
			m.visible = append(m.visible, i)
		}
	}
	m.nameW = 0
	for _, i := range m.visible {
		if n := len([]rune(m.items[i].ID)); n > m.nameW {
			m.nameW = n
		}
	}
	if m.nameW > 24 {
		m.nameW = 24
	}
	m.clamp()
}

// clamp keeps the cursor within the visible range and the scroll offset such
// that the cursor stays on screen.
func (m *selectorModel) clamp() {
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

func (m *selectorModel) moveUp() {
	if m.cursor > 0 {
		m.cursor--
		m.clamp()
	}
}

func (m *selectorModel) moveDown() {
	if m.cursor < len(m.visible)-1 {
		m.cursor++
		m.clamp()
	}
}

// toggle flips the selection of the item under the cursor, if it is valid.
func (m *selectorModel) toggle() {
	if len(m.visible) == 0 {
		return
	}
	orig := m.visible[m.cursor]
	if m.items[orig].Valid {
		m.chosen[orig] = !m.chosen[orig]
	}
}

// Update implements tea.Model: it tracks terminal size, scrolls the viewport,
// drives the filter, and handles toggle/confirm/cancel.
func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

// handleKey dispatches a key press. Arrows, enter, and ctrl+c always act. esc
// is two-stage: it first unfocuses an active filter input (keeping the query so
// the filtered list stays toggleable), then clears an applied query, then
// cancels. Other keys route to the filter input when filtering, else to
// navigation.
func (m selectorModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case keyCtrlC:
		m.cancelled = true
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
		if m.filtering {
			// Commit the filter: unfocus the input but keep the query, so the
			// filtered list stays navigable and toggleable (FR-006).
			m.filtering = false
			return m, nil
		}
		if m.filter.value != "" {
			// Clear an applied filter back to the full list.
			m.filter.value = ""
			m.recomputeVisible()
			return m, nil
		}
		m.cancelled = true
		return m, tea.Quit
	}

	if m.filtering {
		return m.handleFilterKey(key)
	}
	return m.handleNavKey(key)
}

// handleFilterKey edits the filter query through the shared line editor and
// recomputes the visible set after each edit. Space is not query text: it
// toggles the highlighted row, so multi-select works mid-search without an
// esc first (keyboard contract: space toggles on every step).
func (m selectorModel) handleFilterKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
func (m selectorModel) handleNavKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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

// View implements tea.Model: it renders the header, the body (filter line and
// bounded scrolling window), and a position + help footer.
func (m selectorModel) View() string {
	var b strings.Builder
	b.WriteString(m.st.Title.Render("Select skills to install:") + "\n")
	b.WriteString(m.viewBody())
	fmt.Fprintf(&b, "\n%s  •  %s\n",
		m.st.Badge.Render(m.position()),
		m.st.Hint.Render("↑/↓ move · space toggle · / filter · enter confirm · q cancel"))
	return b.String()
}

// viewBody renders the filter line and the bounded, scrolling row window —
// everything between the title and the footer — so embedders (the wizard's
// selection step) can frame it without string surgery.
func (m selectorModel) viewBody() string {
	var b strings.Builder
	if m.filtering || m.filter.value != "" {
		fmt.Fprintf(&b, "%s %s\n", m.st.Accent.Render("Filter:"), m.filter.value)
	} else {
		b.WriteString("\n")
	}

	if len(m.visible) == 0 {
		b.WriteString(m.emptyMessage())
		return b.String()
	}
	// Render only the visible window: building all row strings per frame is
	// O(catalog) waste at 200+ skills (review finding).
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

// position renders the cursor position, filtered/total counts, and the running
// selection count — kept visible while a filter hides selected rows.
func (m selectorModel) position() string {
	pos := fmt.Sprintf("%d/%d", min(m.cursor+1, len(m.visible)), len(m.visible))
	if len(m.visible) != len(m.items) {
		pos += fmt.Sprintf(" (of %d)", len(m.items))
	}
	if n := m.chosenCount(); n > 0 {
		pos += fmt.Sprintf(" · %d selected", n)
	}
	return pos
}

// chosenCount counts the selected valid items without allocating.
func (m selectorModel) chosenCount() int {
	n := 0
	for i, on := range m.chosen {
		if on && m.items[i].Valid {
			n++
		}
	}
	return n
}

// emptyMessage returns the line shown when no rows are visible, distinguishing a
// genuinely empty discovery from a filter that matched nothing.
func (m selectorModel) emptyMessage() string {
	if len(m.items) == 0 {
		return "  (no skills discovered)\n"
	}
	return "  (no matches)\n"
}

// rowString renders one row as aligned columns — checkbox, name (padded to
// the shared column width), description, and in-repo path (FR-009). Invalid
// rows render struck-through with ✗ and show their reason under the cursor
// (FR-011).
func (m selectorModel) rowString(vi int) string {
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

	name := it.ID
	if pad := m.nameW - len([]rune(name)); pad > 0 {
		name += strings.Repeat(" ", pad)
	}
	path := it.RepoPath
	if path == "" {
		path = "."
	}

	if !it.Valid {
		check = "[✗] "
		reason := "invalid"
		if vi == m.cursor && it.InvalidReason != "" {
			reason = "invalid: " + it.InvalidReason
		}
		return m.truncateToWidth(cursor + m.st.Invalid.Render(check+name+"  "+reason+"  "+path))
	}

	if vi == m.cursor {
		name = m.st.Selected.Render(name)
	}
	row := cursor + check + name + "  " + m.st.Subtitle.Render(it.Description) + "  " + m.st.Hint.Render(path)
	return m.truncateToWidth(row)
}

// truncateToWidth shortens a row to the terminal width (when known) so a long
// id or path cannot soft-wrap and push rows past the bounded viewport height.
// Truncation is ANSI-aware: styled rows carry escape sequences that must not
// count against the visible width.
func (m selectorModel) truncateToWidth(s string) string {
	if m.width <= 0 {
		return s
	}
	return ansi.Truncate(s, m.width, "…")
}

// chosenIndices returns the original indices of selected valid items in order.
func (m selectorModel) chosenIndices() []int {
	var out []int
	for i := range m.items {
		if m.chosen[i] && m.items[i].Valid {
			out = append(out, i)
		}
	}
	return out
}
