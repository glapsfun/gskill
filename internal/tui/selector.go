package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	query     string
	visible   []int // original indices matching query, in order
	done      bool
	cancelled bool
}

func newSelectorModel(items []SkillItem) selectorModel {
	m := selectorModel{items: items, chosen: make(map[int]bool)}
	m.recomputeVisible()
	return m
}

// Init implements tea.Model.
func (m selectorModel) Init() tea.Cmd { return nil }

// pageSize returns the number of skill rows that fit on screen, never below 1.
func (m selectorModel) pageSize() int {
	if m.height <= 0 {
		return defaultPageSize
	}
	if p := m.height - reservedRows; p > 0 {
		return p
	}
	return 1
}

// recomputeVisible rebuilds the filtered index list from the current query
// (case-insensitive substring match over ID, RepoPath, and Description —
// FR-010) and re-clamps the cursor and scroll offset. It never touches the
// selection set.
func (m *selectorModel) recomputeVisible() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	m.visible = m.visible[:0]
	for i, it := range m.items {
		if q == "" ||
			strings.Contains(strings.ToLower(it.ID), q) ||
			strings.Contains(strings.ToLower(it.RepoPath), q) ||
			strings.Contains(strings.ToLower(it.Description), q) {
			m.visible = append(m.visible, i)
		}
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
		if m.query != "" {
			// Clear an applied filter back to the full list.
			m.query = ""
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

// handleFilterKey edits the filter query. Printable runes (and space) are
// appended; backspace trims. The visible set is recomputed after each edit.
func (m selectorModel) handleFilterKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type { //nolint:exhaustive // text entry intentionally handles only these keys; all others are ignored
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.recomputeVisible()
		}
	case tea.KeySpace:
		m.query += " "
		m.recomputeVisible()
	case tea.KeyRunes:
		m.query += string(key.Runes)
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

// View implements tea.Model: it renders the header, an optional filter line,
// the bounded scrolling window with more-above/below markers, and a position +
// help footer.
func (m selectorModel) View() string {
	var b strings.Builder
	b.WriteString("Select skills to install:\n")

	if m.filtering || m.query != "" {
		fmt.Fprintf(&b, "Filter: %s\n", m.query)
	} else {
		b.WriteString("\n")
	}

	if len(m.visible) == 0 {
		b.WriteString(m.emptyMessage())
	} else {
		ps := m.pageSize()
		end := m.offset + ps
		if end > len(m.visible) {
			end = len(m.visible)
		}
		if m.offset > 0 {
			b.WriteString("  ↑ more\n")
		}
		for vi := m.offset; vi < end; vi++ {
			m.writeRow(&b, vi)
		}
		if end < len(m.visible) {
			b.WriteString("  ↓ more\n")
		}
	}

	fmt.Fprintf(&b, "\n%d/%d", min(m.cursor+1, len(m.visible)), len(m.visible))
	if len(m.visible) != len(m.items) {
		fmt.Fprintf(&b, " (of %d)", len(m.items))
	}
	b.WriteString("  •  ↑/↓ move · space toggle · / filter · enter confirm · q cancel\n")
	return b.String()
}

// emptyMessage returns the line shown when no rows are visible, distinguishing a
// genuinely empty discovery from a filter that matched nothing.
func (m selectorModel) emptyMessage() string {
	if len(m.items) == 0 {
		return "  (no skills discovered)\n"
	}
	return "  (no matches)\n"
}

// writeRow renders a single skill row at the given index into visible: the
// checkbox, id, in-repo path, and the short description (FR-009). An invalid
// row under the cursor also shows its reason (FR-011).
func (m selectorModel) writeRow(b *strings.Builder, vi int) {
	orig := m.visible[vi]
	it := m.items[orig]
	cursor := " "
	if vi == m.cursor {
		cursor = ">"
	}
	check := "[ ]"
	if m.chosen[orig] {
		check = "[x]"
	}
	suffix := ""
	if !it.Valid {
		check = "[-]"
		suffix = " (invalid)"
		if vi == m.cursor && it.InvalidReason != "" {
			suffix = " (invalid: " + it.InvalidReason + ")"
		}
	}
	path := it.RepoPath
	if path == "" {
		path = "."
	}
	row := fmt.Sprintf("%s %s %s  %s%s", cursor, check, it.ID, path, suffix)
	if it.Description != "" {
		row += "  — " + it.Description
	}
	b.WriteString(m.truncateToWidth(row))
	b.WriteByte('\n')
}

// truncateToWidth shortens a row to the terminal width (when known) so a long
// id or path cannot soft-wrap and push rows past the bounded viewport height.
func (m selectorModel) truncateToWidth(s string) string {
	if m.width <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= m.width {
		return s
	}
	if m.width == 1 {
		return string(r[:1])
	}
	return string(r[:m.width-1]) + "…"
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
