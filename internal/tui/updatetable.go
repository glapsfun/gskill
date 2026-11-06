package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/glapsfun/gskill/internal/errs"
)

// UpdateTable is the input of the grouped update table (spec 032 US1): every
// tracked skill, grouped by repository, plus what carries over from the
// previous round of the same session.
type UpdateTable struct {
	Groups []TableGroup
	// Selected carries the previous round's selection; ineligible names are
	// ignored, so a skill the run updated drops out.
	Selected []string
	// Cursor names the skill or group key to start on.
	Cursor string
	// Banner is the previous round's one-line summary.
	Banner string
	DryRun bool
}

// TableGroup is one repository's skills.
type TableGroup struct {
	Key     string
	Label   string
	Local   bool
	Summary string
	Rows    []TableRow
}

// TableRow is one tracked skill. Only eligible rows can be selected.
type TableRow struct {
	Name      string
	Path      string
	Current   string
	Candidate string
	Policy    string
	Status    string
	Detail    string
	Eligible  bool
}

// UpdateTableResult is the outcome of one table round. Cancelled (esc/q) and
// Interrupted (ctrl-c) both mean "apply nothing".
type UpdateTableResult struct {
	Names       []string
	Cancelled   bool
	Interrupted bool
	// Cursor is where the cursor was, for the next round.
	Cursor string
}

// SelectGroupedUpdates runs one round of the grouped update table and returns
// the selection confirmed in its review. It refuses to start without a TTY:
// the CLI's interactivity gate decides when the table may open.
func SelectGroupedUpdates(t UpdateTable, isTTY bool) (UpdateTableResult, error) {
	if !isTTY {
		return UpdateTableResult{}, fmt.Errorf("%w: interactive update selection requires a TTY; pass skill names, --repo, or --no-interactive", errs.ErrUsage)
	}
	final, err := tea.NewProgram(newUpdateTableModel(t)).Run()
	if err != nil {
		return UpdateTableResult{}, fmt.Errorf("tui: %w", err)
	}
	m, ok := final.(updateTableModel)
	if !ok {
		return UpdateTableResult{Cancelled: true}, nil
	}
	return m.result(), nil
}

// tableReservedRows is the number of non-list lines framing the table body:
// the title, the filter-or-blank line, the more-above and more-below
// markers, a blank spacer, the detail line, and the footer. A banner adds one.
const tableReservedRows = 7

// reviewReservedRows frames the review body: the title, a blank line, the
// more markers, a blank line, and the footer. The dry-run notice adds one.
const reviewReservedRows = 6

// tableLine is one line of the table body: a group header (row < 0) or a
// skill row, as indices into the groups.
type tableLine struct{ group, row int }

func (l tableLine) header() bool { return l.row < 0 }

// updateTableModel is the grouped table and review state. Selection is keyed
// by skill name, never by position, so filtering, collapsing, scrolling, and
// a refresh between rounds cannot move it (spec 032 FR-017).
type updateTableModel struct {
	groups       []TableGroup
	chosen       map[string]bool
	collapsed    map[string]bool // keyed by group key
	filtering    bool            // the filter input has focus
	filter       lineInput
	lines        []tableLine
	cursor       int // index into lines
	offset       int // index into lines of the first on-screen body line
	width        int
	height       int
	reviewing    bool
	reviewOffset int
	banner       string
	dryRun       bool
	message      string // one-shot detail text, cleared by the next key
	cols         tableCols
	done         bool
	cancelled    bool
	interrupted  bool
	st           Theme
}

// tableCols holds the aligned column widths, computed over every row so the
// layout does not jump as the view changes.
type tableCols struct{ name, transition, policy, status, path int }

func newUpdateTableModel(t UpdateTable) updateTableModel {
	m := updateTableModel{
		groups: t.Groups, chosen: make(map[string]bool), collapsed: make(map[string]bool),
		banner: t.Banner, dryRun: t.DryRun, st: DefaultTheme(),
	}
	eligible := map[string]bool{}
	for _, g := range m.groups {
		for _, r := range g.Rows {
			eligible[r.Name] = r.Eligible
			m.cols.name = max(m.cols.name, ansi.StringWidth(r.Name))
			m.cols.transition = max(m.cols.transition, ansi.StringWidth(transition(r)))
			m.cols.policy = max(m.cols.policy, ansi.StringWidth(r.Policy))
			m.cols.status = max(m.cols.status, ansi.StringWidth(r.Status))
			m.cols.path = max(m.cols.path, ansi.StringWidth(r.Path))
		}
	}
	m.cols.name = min(m.cols.name, nameColMax)
	for _, name := range t.Selected {
		if eligible[name] {
			m.chosen[name] = true
		}
	}
	m.rebuildLines()
	for i := range m.lines {
		if m.lineID(i) == t.Cursor {
			m.cursor = i
			break
		}
	}
	m.clamp()
	return m
}

// transition renders a row's "current → candidate" cell.
func transition(r TableRow) string {
	if r.Candidate == "" {
		return r.Current
	}
	return r.Current + " → " + r.Candidate
}

// rebuildLines lays out the body lines for the current filter and collapse
// state, keeping the cursor on the same skill or group when it is still
// shown. It never touches the selection.
func (m *updateTableModel) rebuildLines() {
	keep := m.cursorID()
	m.lines = m.lines[:0]
	for gi, g := range m.groups {
		header := len(m.lines)
		for ri, r := range g.Rows {
			if !m.passes(gi, r) {
				continue
			}
			if header == len(m.lines) {
				m.lines = append(m.lines, tableLine{group: gi, row: -1})
			}
			if !m.collapsed[g.Key] {
				m.lines = append(m.lines, tableLine{group: gi, row: ri})
			}
		}
	}
	for i := range m.lines {
		if m.lineID(i) == keep {
			m.cursor = i
			break
		}
	}
	m.clamp()
}

// passes reports whether a row matches the filter: a case-insensitive
// substring of its name, its path, or its group's label. A label match
// passes the whole group (spec 032 US4 scenario 2).
func (m updateTableModel) passes(gi int, r TableRow) bool {
	q := strings.ToLower(strings.TrimSpace(m.filter.value))
	return q == "" ||
		strings.Contains(strings.ToLower(m.groups[gi].Label), q) ||
		strings.Contains(strings.ToLower(r.Name), q) ||
		strings.Contains(strings.ToLower(r.Path), q)
}

// collapsedAt reports whether the group with key is collapsed.
func (m updateTableModel) collapsedAt(key string) bool { return m.collapsed[key] }

// Init implements tea.Model.
func (m updateTableModel) Init() tea.Cmd { return nil }

func (m updateTableModel) reserved() int {
	if m.banner != "" {
		return tableReservedRows + 1
	}
	return tableReservedRows
}

// showFilter reports whether the filter line replaces the blank line.
func (m updateTableModel) showFilter() bool { return m.filtering || m.filter.value != "" }

func (m updateTableModel) pageSize() int { return pageFor(m.height, m.reserved()) }

// clamp keeps the cursor in range and on screen.
func (m *updateTableModel) clamp() {
	if len(m.lines) == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	m.cursor = min(max(m.cursor, 0), len(m.lines)-1)
	ps := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+ps {
		m.offset = m.cursor - ps + 1
	}
	m.offset = min(m.offset, max(len(m.lines)-ps, 0))
	m.offset = max(m.offset, 0)
}

// lineID names the skill or group key on body line i.
func (m updateTableModel) lineID(i int) string {
	if i < 0 || i >= len(m.lines) {
		return ""
	}
	l := m.lines[i]
	if l.header() {
		return m.groups[l.group].Key
	}
	return m.groups[l.group].Rows[l.row].Name
}

// cursorID names the skill or group key under the cursor.
func (m updateTableModel) cursorID() string { return m.lineID(m.cursor) }

// Update implements tea.Model.
func (m updateTableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == keyCtrlC {
			m.interrupted = true
			return m, tea.Quit
		}
		m.message = ""
		if m.reviewing {
			return m.handleReviewKey(msg)
		}
		return m.handleTableKey(msg)
	}
	return m, nil
}

// handleReviewKey: enter applies, esc and q cancel (spec 032 FR-013, FR-015).
func (m updateTableModel) handleReviewKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case keyEnter:
		m.done = true
		return m, tea.Quit
	case keyEsc, "q":
		m.cancelled = true
		return m, tea.Quit
	case keyUp, "k":
		m.reviewOffset = max(m.reviewOffset-1, 0)
	case keyDown, "j":
		m.reviewOffset++
	}
	return m, nil
}

// handleTableKey handles the table screen's keys. Enter, the arrows, and
// esc work the same with the filter focused; other keys edit the query then.
func (m updateTableModel) handleTableKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case keyEnter, keyUp, keyDown, "left", "right":
		return m.handleNavKey(key)
	case keyEsc:
		return m.handleEsc()
	}
	if m.filtering {
		if key.String() == " " {
			m.toggle()
		} else if m.filter.handleKey(key) {
			m.rebuildLines()
		}
		return m, nil
	}
	return m.handleNavKey(key)
}

// handleEsc is staged: leave the filter input, then clear the filter, then
// cancel (spec 032 FR-018).
func (m updateTableModel) handleEsc() (tea.Model, tea.Cmd) {
	switch {
	case m.filtering:
		m.filtering = false
	case m.filter.value != "":
		m.filter.value = ""
		m.rebuildLines()
	default:
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// handleNavKey handles the table keys outside text entry.
func (m updateTableModel) handleNavKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case keyEnter:
		if len(m.chosen) == 0 {
			m.done = true
			return m, tea.Quit
		}
		m.reviewing, m.reviewOffset = true, 0
	case keyUp, "k":
		m.cursor--
		m.clamp()
	case keyDown, "j":
		m.cursor++
		m.clamp()
	case "home":
		m.cursor = 0
		m.clamp()
	case "end":
		m.cursor = len(m.lines) - 1
		m.clamp()
	case " ", "x":
		m.toggle()
	case "a":
		m.toggleScope(m.scopeNames(-1), "")
	case "left", "h":
		m.fold(true)
	case "right", "l":
		m.fold(false)
	case "/":
		m.filtering = true
	case "q":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// fold collapses or expands the cursor's group and leaves the cursor on its
// header. Collapsing never changes the selection.
func (m *updateTableModel) fold(collapse bool) {
	if len(m.lines) == 0 {
		return
	}
	gi := m.lines[m.cursor].group
	key := m.groups[gi].Key
	if collapse {
		m.collapsed[key] = true
	} else {
		delete(m.collapsed, key)
	}
	for i, l := range m.lines {
		if l.group == gi {
			m.cursor = i
			break
		}
	}
	m.rebuildLines()
}

// toggle flips the row under the cursor when it is eligible, or the whole
// group when the cursor is on its header (spec 032 FR-011).
func (m *updateTableModel) toggle() {
	if len(m.lines) == 0 {
		return
	}
	l := m.lines[m.cursor]
	if l.header() {
		m.toggleScope(m.scopeNames(l.group), m.groups[l.group].Label)
		return
	}
	r := m.groups[l.group].Rows[l.row]
	if !r.Eligible {
		return
	}
	if m.chosen[r.Name] {
		delete(m.chosen, r.Name)
	} else {
		m.chosen[r.Name] = true
	}
}

// toggleScope selects every name unless all of them are already selected,
// in which case it clears them. An empty scope selects nothing and says so.
func (m *updateTableModel) toggleScope(names []string, label string) {
	if len(names) == 0 {
		if label == "" {
			m.message = "no updates available"
		} else {
			m.message = label + " has no updates available"
		}
		return
	}
	all := true
	for _, n := range names {
		all = all && m.chosen[n]
	}
	for _, n := range names {
		if all {
			delete(m.chosen, n)
		} else {
			m.chosen[n] = true
		}
	}
}

// scopeNames lists the eligible skills a group toggle (gi >= 0) or the
// project toggle (gi < 0) acts on.
func (m updateTableModel) scopeNames(gi int) []string {
	var names []string
	for i, g := range m.groups {
		if gi >= 0 && i != gi {
			continue
		}
		for _, r := range g.Rows {
			if r.Eligible && m.inScope(i, r) {
				names = append(names, r.Name)
			}
		}
	}
	return names
}

// inScope reports whether a row is in reach of the group and project
// toggles: every row the filter passes, collapsed or not (spec 032 FR-012).
func (m updateTableModel) inScope(gi int, r TableRow) bool { return m.passes(gi, r) }

// selectedIn counts the selected skills of group gi.
func (m updateTableModel) selectedIn(gi int) int {
	n := 0
	for _, r := range m.groups[gi].Rows {
		if m.chosen[r.Name] {
			n++
		}
	}
	return n
}

// result returns the confirmed names in table order, or the cancel flags.
func (m updateTableModel) result() UpdateTableResult {
	res := UpdateTableResult{Cancelled: m.cancelled, Interrupted: m.interrupted, Cursor: m.cursorID()}
	if res.Cancelled || res.Interrupted {
		return res
	}
	for _, g := range m.groups {
		for _, r := range g.Rows {
			if m.chosen[r.Name] {
				res.Names = append(res.Names, r.Name)
			}
		}
	}
	return res
}

// fit truncates one rendered line to the terminal width, so a narrow
// terminal never soft-wraps the frame.
func (m updateTableModel) fit(s string) string {
	if m.width <= 0 {
		return s
	}
	return ansi.Truncate(s, m.width, "…")
}

// View implements tea.Model.
func (m updateTableModel) View() string {
	if m.reviewing {
		return m.reviewView()
	}
	var b strings.Builder
	line := func(s string) {
		b.WriteString(m.fit(s))
		b.WriteByte('\n')
	}
	line(m.st.Title.Render(m.title()))
	if m.banner != "" {
		line(m.st.Success.Render(m.banner))
	}
	if m.showFilter() {
		line(m.st.Accent.Render("Filter:") + " " + m.filter.value)
	} else {
		line("")
	}

	start, end, above, below := windowBounds(len(m.lines), m.pageSize(), m.offset)
	switch {
	case above && !m.lines[start].header():
		// Scrolled into a group: its header stays in place of the marker so
		// the rows below always say which repository they belong to.
		line(m.headerLine(m.lines[start].group, false) + m.st.Hint.Render(" ↑"))
	case above:
		line(m.st.Hint.Render("  ↑ more"))
	}
	for i := start; i < end; i++ {
		line(m.bodyLine(i))
	}
	if below {
		line(m.st.Hint.Render("  ↓ more"))
	}

	line("")
	line(m.st.Hint.Render(m.detail()))
	line(m.st.Badge.Render(m.position()) + "  •  " + m.st.Hint.Render(m.help()))
	return b.String()
}

// title is the table's first line; it says so when nothing can be updated
// (spec 032 FR-019).
func (m updateTableModel) title() string {
	for _, g := range m.groups {
		for _, r := range g.Rows {
			if r.Eligible {
				return "Select updates (space toggles, enter reviews):"
			}
		}
	}
	return "No updates available. Every skill is shown with its status."
}

func (m updateTableModel) help() string {
	return "↑/↓ move · space toggle · a all · ←/→ fold · / filter · enter review · esc cancel"
}

// position renders the cursor position, the selection count, and how many
// selected skills the filter or a collapse hides (spec 032 FR-017).
func (m updateTableModel) position() string {
	pos := fmt.Sprintf("%d/%d", min(m.cursor+1, len(m.lines)), len(m.lines))
	if n := len(m.chosen); n > 0 {
		pos += fmt.Sprintf(" · %d selected", n)
		if hidden := n - m.selectedShown(); hidden > 0 {
			pos += fmt.Sprintf(" (%d hidden)", hidden)
		}
	}
	return pos
}

// selectedShown counts the selected skills that have a body line.
func (m updateTableModel) selectedShown() int {
	n := 0
	for _, l := range m.lines {
		if !l.header() && m.chosen[m.groups[l.group].Rows[l.row].Name] {
			n++
		}
	}
	return n
}

// detail is the line under the table: a one-shot message, else the cursor
// row's reason and guidance, else the cursor group's counts.
func (m updateTableModel) detail() string {
	if m.message != "" {
		return m.message
	}
	if len(m.lines) == 0 {
		return ""
	}
	l := m.lines[m.cursor]
	g := m.groups[l.group]
	if l.header() {
		return g.Summary
	}
	r := g.Rows[l.row]
	if r.Detail == "" {
		return ""
	}
	return r.Name + ": " + r.Detail
}

// bodyLine renders body line i: a group header or an aligned skill row.
func (m updateTableModel) bodyLine(i int) string {
	l := m.lines[i]
	g := m.groups[l.group]
	cursor := "  "
	if i == m.cursor {
		cursor = m.st.Cursor.Render("❯") + " "
	}
	if l.header() {
		return m.headerLine(l.group, i == m.cursor)
	}

	r := g.Rows[l.row]
	check := "    "
	if r.Eligible {
		check = "[ ] "
		if m.chosen[r.Name] {
			check = m.st.Success.Render("[✓]") + " "
		}
	}
	name := padCell(r.Name, m.cols.name)
	if i == m.cursor {
		name = m.st.Selected.Render(name)
	}
	status := m.st.Subtitle.Render(r.Status)
	if r.Eligible {
		status = m.st.Warning.Render(r.Status)
	}
	showPolicy, showPath := m.columnsFit()
	cells := []string{name, padCell(transition(r), m.cols.transition)}
	if showPolicy {
		cells = append(cells, m.st.Subtitle.Render(padCell(r.Policy, m.cols.policy)))
	}
	if showPath && r.Path != "" {
		cells = append(cells, status+strings.Repeat(" ", m.cols.status-ansi.StringWidth(r.Status)), m.st.Hint.Render(r.Path))
	} else {
		cells = append(cells, status)
	}
	return cursor + check + strings.Join(cells, "  ")
}

// columnsFit decides which low-priority columns fit the terminal: the path
// drops first, then the policy, before anything is truncated (spec 032
// research R15). The cursor row's detail line still shows both.
func (m updateTableModel) columnsFit() (policy, path bool) {
	if m.width <= 0 {
		return true, true
	}
	base := 2 + 4 + m.cols.name + 2 + m.cols.transition + 2 + m.cols.status
	policy = base+2+m.cols.policy <= m.width
	path = policy && m.cols.path > 0 && base+2+m.cols.policy+2+m.cols.path <= m.width
	return policy, path
}

// headerLine renders group gi's header: fold mark, counts, and selection.
func (m updateTableModel) headerLine(gi int, atCursor bool) string {
	g := m.groups[gi]
	cursor := "  "
	if atCursor {
		cursor = m.st.Cursor.Render("❯") + " "
	}
	mark := "▾ "
	if m.collapsed[g.Key] {
		mark = "▸ "
	}
	text := mark + g.Summary
	if g.Local {
		text += " · local"
	}
	if n := m.selectedIn(gi); n > 0 {
		text += fmt.Sprintf(" · %d selected", n)
	}
	return cursor + m.st.Accent.Render(text)
}

// padCell truncates s to w columns and pads it to exactly w.
func padCell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// reviewView lists the selection grouped by repository with each skill's
// transition. Confirming it is the only confirmation (spec 032 FR-013).
func (m updateTableModel) reviewView() string {
	var body []string
	for _, g := range m.groups {
		var rows []string
		for _, r := range g.Rows {
			if m.chosen[r.Name] {
				rows = append(rows, "  "+padCell(r.Name, m.cols.name)+"  "+transition(r))
			}
		}
		if len(rows) > 0 {
			body = append(body, m.st.Accent.Render(g.Label))
			body = append(body, rows...)
		}
	}

	reserved := reviewReservedRows
	if m.dryRun {
		reserved++
	}
	var b strings.Builder
	line := func(s string) {
		b.WriteString(m.fit(s))
		b.WriteByte('\n')
	}
	n := len(m.chosen)
	noun := "updates"
	if n == 1 {
		noun = "update"
	}
	line(m.st.Title.Render(fmt.Sprintf("Review %d %s:", n, noun)))
	line("")
	for _, l := range windowRows(body, pageFor(m.height, reserved), min(m.reviewOffset, len(body)), m.st.Hint) {
		line(l)
	}
	line("")
	if m.dryRun {
		line(m.st.Warning.Render("Dry run: nothing will be written."))
	}
	line(m.st.Hint.Render("enter apply · esc cancel"))
	return b.String()
}
