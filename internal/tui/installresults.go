package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/app"
)

// Final install-result screen (spec 014 US2): truthful summary counters, a
// themed table of unsuccessful entries (all entries for a dry run, FR-017),
// and a per-row detail view with the complete failure information (FR-019).

// plannedTitles translates dry-run planned actions into display text.
var plannedTitles = map[string]string{
	app.PlannedWouldInstall:      "Would install",
	app.PlannedWouldRepair:       "Would repair",
	app.PlannedWouldRemoveTarget: "Would remove target",
	app.PlannedWouldUpdateLock:   "Would update lock",
	app.PlannedBlocked:           "Blocked",
}

// PlannedTitle returns the display text for a dry-run planned action; unknown
// actions render their wire value so a new action is never hidden. Shared by
// the TUI table and the plain CLI renderer.
func PlannedTitle(action string) string {
	if t, ok := plannedTitles[action]; ok {
		return t
	}
	return action
}

// InstallHeadline maps a run's outcome onto the summary headline. One copy of
// this user-facing contract text serves the TUI and the plain CLI.
func InstallHeadline(sum app.InstallSummary) string {
	switch sum.Outcome { //nolint:exhaustive // success is the default headline
	case app.InstallOutcomePartial, app.InstallOutcomeFailure:
		return "Installation completed with errors"
	case app.InstallOutcomeCancelled:
		return "Installation cancelled"
	case app.InstallOutcomePlanned:
		return "Installation plan (dry run)"
	default:
		return "Installation complete"
	}
}

// counterStyle selects which theme style a summary counter renders with.
type counterStyle int

const (
	counterSuccess counterStyle = iota
	counterNeutral
	counterWarning
	counterError
)

// installCounters is the single source of the summary-counter vocabulary
// (glyphs, labels, order) shared by the styled TUI summary and the plain CLI
// counter line, so the two surfaces can never drift (FR-016/FR-021).
var installCounters = []struct {
	n      func(app.InstallSummary) int
	format string
	style  counterStyle
}{
	{func(s app.InstallSummary) int { return s.Installed }, "✓ %d installed", counterSuccess},
	{func(s app.InstallSummary) int { return s.Repaired }, "✓ %d repaired", counterSuccess},
	{func(s app.InstallSummary) int { return s.UpToDate }, "● %d already up to date", counterNeutral},
	{func(s app.InstallSummary) int { return s.Skipped }, "◐ %d skipped", counterWarning},
	{func(s app.InstallSummary) int { return s.Planned }, "● %d planned", counterNeutral},
	{func(s app.InstallSummary) int { return s.Failed }, "✗ %d failed", counterError},
	{func(s app.InstallSummary) int { return s.Cancelled }, "○ %d cancelled", counterWarning},
	{func(s app.InstallSummary) int { return s.NotAttempted }, "○ %d not attempted", counterWarning},
}

// InstallCounterLine renders the non-zero counters unstyled ("" when all are
// zero); the glyphs are plain text so the line reads identically without
// color (FR-027). The plain CLI consumes this directly.
func InstallCounterLine(sum app.InstallSummary) string {
	var parts []string
	for _, c := range installCounters {
		if n := c.n(sum); n > 0 {
			parts = append(parts, fmt.Sprintf(c.format, n))
		}
	}
	return strings.Join(parts, "   ")
}

// InstallResults renders the result screen. Value semantics, Bubble Tea
// style: Update and SetSize return the modified copy.
type InstallResults struct {
	st   Theme
	sum  app.InstallSummary
	rows []app.LockSkillResult

	tbl      table.Model
	width    int
	height   int
	inDetail bool
	dryRun   bool
}

// NewInstallResults aggregates the run's results and selects the table rows:
// unsuccessful entries only (clarification #2), or every entry with its
// planned action on a dry run (FR-017 exception).
func NewInstallResults(results []app.LockSkillResult) InstallResults {
	m := InstallResults{st: DefaultTheme(), sum: app.Aggregate(results), width: 80, height: 24}
	for _, r := range results {
		if r.PlannedAction != "" || r.Status == app.LockSkillPlanned {
			m.dryRun = true
			break
		}
	}
	for _, r := range results {
		if m.dryRun || unsuccessful(r.Status) {
			m.rows = append(m.rows, r)
		}
	}
	m.rebuildTable()
	return m
}

// unsuccessful reports whether a status belongs in the (non-dry-run) table.
func unsuccessful(status string) bool {
	s := app.InstallStatus(status)
	return s == app.InstallStatusFailed ||
		s == app.InstallStatusCancelled ||
		s == app.InstallStatusNotAttempted
}

// HasRows reports whether the table has anything to show.
func (m InstallResults) HasRows() bool { return len(m.rows) > 0 }

// Cursor exposes the selected row index for tests and hosts.
func (m InstallResults) Cursor() int { return m.tbl.Cursor() }

// Summary exposes the aggregated counters.
func (m InstallResults) Summary() app.InstallSummary { return m.sum }

// SetSize adapts the table to the terminal. Unchanged dimensions are a no-op
// so resize-event bursts don't rebuild identical tables.
func (m InstallResults) SetSize(w, h int) InstallResults {
	if w <= 0 {
		w = m.width
	}
	if h <= 0 {
		h = m.height
	}
	if w == m.width && h == m.height {
		return m
	}
	m.width, m.height = w, h
	m.rebuildTable()
	return m
}

// rebuildTable lays the rows out for the current width. Cells are sanitized
// here — the table is the render boundary for untrusted text (FR-028).
func (m *InstallResults) rebuildTable() {
	statusW := 14
	if m.dryRun {
		statusW = 20
	}
	skillW, sourceW := 16, 22
	if m.width < 100 {
		skillW, sourceW = 13, 16
	}
	versionW, phaseW := 9, 10
	reasonW := max(12, m.width-statusW-skillW-sourceW-versionW-phaseW-14)

	cols := []table.Column{
		{Title: "Status", Width: statusW},
		{Title: "Skill", Width: skillW},
		{Title: "Source", Width: sourceW},
		{Title: "Version", Width: versionW},
		{Title: "Phase", Width: phaseW},
		{Title: "Reason", Width: reasonW},
	}
	rows := make([]table.Row, 0, len(m.rows))
	for _, r := range m.rows {
		rows = append(rows, table.Row{
			m.statusText(r),
			Sanitize(r.Name),
			OrDash(r.Source),
			OrDash(r.ResolvedVersion),
			OrDash(string(r.Phase)),
			OrDash(reasonOf(r)),
		})
	}
	height := min(len(rows)+1, max(4, m.height-10))
	cursor := m.tbl.Cursor()
	m.tbl = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithHeight(height),
		table.WithFocused(true),
	)
	m.tbl.SetStyles(m.st.TableStyles())
	if cursor > 0 && cursor < len(rows) {
		m.tbl.SetCursor(cursor)
	}
}

// statusText is a row's textual state — planned actions on a dry run, the
// status word otherwise. Never color-only (FR-017).
func (m InstallResults) statusText(r app.LockSkillResult) string {
	if r.PlannedAction != "" {
		return PlannedTitle(r.PlannedAction)
	}
	return r.Status
}

// reasonOf is the one-line reason for the table cell.
func reasonOf(r app.LockSkillResult) string {
	if r.Failure != nil {
		return r.Failure.Message
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	return ""
}

// Update handles the result screen's keys (FR-018). exit=true means the host
// should leave the screen.
func (m InstallResults) Update(msg tea.KeyMsg) (InstallResults, bool) {
	// ctrl+c always exits: Bubble Tea's raw mode never raises SIGINT, so the
	// universal interrupt must be honored as a key from every mode.
	if msg.String() == keyCtrlC {
		return m, true
	}
	if m.inDetail {
		switch msg.String() {
		case keyEsc, "q":
			m.inDetail = false
		}
		return m, false
	}
	switch msg.String() {
	case "enter":
		if len(m.rows) > 0 {
			m.inDetail = true
		}
		return m, false
	case "q", keyEsc:
		return m, true
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	_ = cmd // the table never issues commands
	return m, false
}

// Hints returns the footer hint line for the current mode.
func (m InstallResults) Hints() string {
	if m.inDetail {
		return "esc back"
	}
	if m.HasRows() {
		return "↑/↓ navigate · enter details · q exit"
	}
	return "press any key to exit"
}

// View renders the summary plus the table or the selected row's detail.
func (m InstallResults) View() string {
	if m.inDetail {
		return m.viewDetail()
	}
	var b strings.Builder
	b.WriteString(m.viewSummary())
	if m.HasRows() {
		b.WriteString("\n" + m.tbl.View() + "\n")
	}
	return b.String()
}

// viewSummary renders the outcome headline and the non-zero counters, which
// by construction sum to the total (FR-015/FR-016). The vocabulary comes from
// the shared installCounters table; only the styling is decided here.
func (m InstallResults) viewSummary() string {
	var b strings.Builder
	b.WriteString(m.st.Title.Render(InstallHeadline(m.sum)) + "\n")
	fmt.Fprintf(&b, "%d skills processed\n", m.sum.Total)

	var parts []string
	for _, c := range installCounters {
		if n := c.n(m.sum); n > 0 {
			parts = append(parts, m.counterRender(c.style)(fmt.Sprintf(c.format, n)))
		}
	}
	if len(parts) > 0 {
		b.WriteString(strings.Join(parts, "   ") + "\n")
	}
	return b.String()
}

// counterRender maps a counter's semantic style onto the theme.
func (m InstallResults) counterRender(s counterStyle) func(...string) string {
	switch s {
	case counterSuccess:
		return m.st.Success.Render
	case counterWarning:
		return m.st.Warning.Render
	case counterError:
		return m.st.Error.Render
	case counterNeutral:
		return m.st.Subtitle.Render
	default:
		return m.st.Subtitle.Render
	}
}

// viewDetail renders the selected row's complete information (FR-019); every
// unknown value shows — (FR-014) and every string is sanitized.
func (m InstallResults) viewDetail() string {
	i := m.tbl.Cursor()
	if i < 0 || i >= len(m.rows) {
		return ""
	}
	r := m.rows[i]
	var b strings.Builder
	b.WriteString(m.st.Title.Render(Sanitize(r.Name)) + "\n\n")
	b.WriteString(m.st.Subtitle.Render("Status") + "\n")
	fmt.Fprintf(&b, "  %s\n\n", m.st.InstallStatusCell(Sanitize(m.statusText(r))))
	b.WriteString(m.st.Subtitle.Render("Source") + "\n")
	fmt.Fprintf(&b, "  Repository:  %s\n", OrDash(r.Source))
	fmt.Fprintf(&b, "  Source type: %s\n", OrDash(r.SourceType))
	fmt.Fprintf(&b, "  Skill path:  %s\n", OrDash(r.SkillPath))
	fmt.Fprintf(&b, "  Requested:   %s\n", OrDash(r.RequestedRef))
	fmt.Fprintf(&b, "  Version:     %s\n", OrDash(r.ResolvedVersion))
	fmt.Fprintf(&b, "  Ref:         %s\n", OrDash(r.ResolvedRef))
	fmt.Fprintf(&b, "  Commit:      %s\n", OrDash(r.Commit))
	fmt.Fprintf(&b, "  Agents:      %s\n", OrDash(strings.Join(r.Agents, ", ")))
	fmt.Fprintf(&b, "  Mode:        %s\n", OrDash(r.InstallMode))
	if f := r.Failure; f != nil {
		b.WriteString("\n" + m.st.Subtitle.Render("Failed phase") + "\n")
		fmt.Fprintf(&b, "  %s\n", PhaseTitle(f.Phase))
		b.WriteString("\n" + m.st.Subtitle.Render("Reason") + " " +
			m.st.Error.Render("("+Sanitize(string(f.Category))+")") + "\n")
		fmt.Fprintf(&b, "  %s\n", Sanitize(f.Message))
		if f.Expected != "" || f.Actual != "" {
			b.WriteString("\n" + m.st.Subtitle.Render("Expected") + "\n")
			fmt.Fprintf(&b, "  %s\n", OrDash(f.Expected))
			b.WriteString(m.st.Subtitle.Render("Actual") + "\n")
			fmt.Fprintf(&b, "  %s\n", OrDash(f.Actual))
		}
		if f.Hint != "" {
			b.WriteString("\n" + m.st.Subtitle.Render("Suggested action") + "\n")
			fmt.Fprintf(&b, "  %s\n", Sanitize(f.Hint))
		}
	}
	return b.String()
}
