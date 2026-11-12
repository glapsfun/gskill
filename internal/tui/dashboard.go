package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/glapsfun/gskill/internal/errs"
)

// Run launches the interactive dashboard over svc. It refuses to start without
// a TTY, returning a usage error that points at the equivalent CLI commands
// (spec 033 FR-022).
func Run(ctx context.Context, svc DashboardService, isTTY bool, opts Options) error {
	if !isTTY {
		return fmt.Errorf("%w: the TUI requires a TTY; use 'gskill list' or 'gskill info <name>' instead",
			errs.ErrUsage)
	}
	final, err := tea.NewProgram(newModel(ctx, svc, opts), tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	// The dashboard shows its own results; the session returns one error, mapped
	// once to an exit code by cli/tui.go (FR-025).
	if m, ok := final.(model); ok {
		return m.sessionErr
	}
	return nil
}

// previewMinHeight is the terminal height below which the preview strip
// collapses so the table keeps a usable page (design: 20-row floor).
const previewMinHeight = 20

// dashMode is the dashboard's current interaction mode. Browse and search are
// the read-only modes; confirm, versionPick, busy, and results carry the
// update/version/remove stories.
type dashMode int

const (
	modeBrowse dashMode = iota
	modeSearch
	modeConfirm     // reviewing a planned action before the single confirmation
	modeVersionPick // picking the target of a version change
	modeBusy        // an apply is in flight; action keys are ignored
	modeResults     // the per-skill outcomes of the finished action
)

// model is the dashboard state: a themed, repository-grouped skills table
// stacked over a details line and a bordered preview viewport showing the
// selected skill's SKILL.md. It reads and mutates the project only through
// svc (spec 033).
type model struct {
	svc  DashboardService
	opts Options
	// ctx is the run context carried into the async service commands.
	ctx context.Context //nolint:containedctx // Bubble Tea commands are built from model state; the run context must travel with the model
	st  Theme
	tbl table.Model
	vp  viewport.Model

	focusPreview bool
	// previewCache memoizes rendered SKILL.md previews per skill name for the
	// current viewport width (cleared on resize): glamour builds a fresh
	// renderer per call, which is too slow to repeat on every cursor move.
	previewCache map[string]string

	width, height int

	// skills is the full installed inventory in repository-group order;
	// visible indexes into it for the current search; groups describes the
	// group boundaries. selected is the chosen skill's name, held by identity
	// so it survives searching and refreshing (FR-017).
	skills   []DashboardSkill
	visible  []int
	groups   []RepoGroup
	selected string

	mode    dashMode
	query   lineInput
	loaded  bool
	loadErr string

	// Action state. actionName and actionKind name the skill (or repository
	// key, for a group update) the pending confirm/version flow targets, held
	// by identity so a search change or refresh never retargets it (FR-017).
	// proposed is the reviewed, not-yet-applied change; nothing is written
	// before the confirm -> busy edge. inflight is the identity of the apply
	// running, which blocks a duplicate or conflicting submission (FR-018).
	actionName string
	actionKind ChangeKind
	// actionFrom is the selected skill's installed-version label captured when
	// the action began. The stale guard compares it against the current label at
	// confirm time (FR-019); both come from the same List/displayVersion source,
	// so a branch skill (label "main") is not mistaken for changed against a
	// plan's commit-shaped revision.
	actionFrom  string
	proposed    *ProposedChange
	planLoading bool
	planErr     string
	inflight    string
	results     []ActionOutcome
	resultErr   string

	// Version picker state: the candidate labels, the cursor, and a typed
	// target for an explicit version the list does not offer.
	versions   []string
	verCursor  int
	verInput   lineInput
	verLoading bool
	verErr     string

	// sessionErr is the one error returned to cli/tui.go on quit, mapped once
	// to an exit code (FR-025); a ctrl+c quit sets it to ErrCancelled (code 130).
	sessionErr error
}

// inventoryMsg carries a loaded (or refreshed) inventory into the model.
type inventoryMsg struct {
	skills []DashboardSkill
}

// dashErrMsg carries an async failure (discovery or a mutation) for display.
type dashErrMsg struct {
	err error
}

// newModel builds the dashboard model. The inventory loads asynchronously via
// Init so the first paint never blocks on discovery (FR-018).
func newModel(ctx context.Context, svc DashboardService, opts Options) model {
	st := DefaultTheme()
	tbl := table.New(
		table.WithColumns(dashColumns(defaultWidth)),
		table.WithFocused(true),
	)
	tbl.SetStyles(st.TableStyles())
	return model{
		svc: svc, opts: opts, ctx: ctx, st: st, tbl: tbl,
		vp:           viewport.New(defaultWidth, 6),
		previewCache: make(map[string]string),
		width:        defaultWidth,
	}
}

// loadInventory asks the service for the inventory off the UI goroutine.
func (m model) loadInventory() tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		skills, err := svc.Inventory(ctx)
		if err != nil {
			return dashErrMsg{err: err}
		}
		return inventoryMsg{skills: skills}
	}
}

// dashColumns sizes the table columns for a terminal width: VERSION and
// STATUS are fixed, NAME and SOURCE flex. pad reserves the per-column cell
// padding bubbles renders (2 × 4) plus a right margin (6).
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

// setInventory stores a loaded inventory, rebuilds group order and the visible
// set under the current query, and keeps the selection on the same skill when
// it is still present (FR-009, FR-016, FR-017).
func (m *model) setInventory(skills []DashboardSkill) {
	m.skills = groupOrdered(skills)
	m.groups = groupsOf(m.skills)
	m.loaded = true
	m.loadErr = ""
	m.recomputeVisible()
}

// recomputeVisible refilters the inventory by the current query and syncs the
// table rows and the selection.
func (m *model) recomputeVisible() {
	q := strings.ToLower(strings.TrimSpace(m.query.value))
	m.visible = m.visible[:0]
	for i, s := range m.skills {
		if q == "" || strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.SourceLabel), q) {
			m.visible = append(m.visible, i)
		}
	}
	m.tbl.SetRows(m.visibleRows())
	m.syncCursorToSelection()
	m.refreshPreview()
}

// visibleRows renders the filtered skills as sanitized table rows. Cells must
// stay ANSI-free: bubbles/table truncates by byte width, so a styled cell
// bleeds color into the rest of the table. The STATUS column shows Health
// (FR-001); UpdateStatus drives actions, not the column.
func (m model) visibleRows() []table.Row {
	out := make([]table.Row, 0, len(m.visible))
	for _, i := range m.visible {
		s := m.skills[i]
		out = append(out, table.Row{
			Sanitize(s.Name), Sanitize(s.InstalledVersion), Sanitize(s.SourceLabel),
			ansi.Strip(m.st.StatusCell(Sanitize(s.Health))),
		})
	}
	return out
}

// syncCursorToSelection keeps the table cursor on the selected skill when it
// is visible; otherwise it adopts the skill under the cursor as the selection.
func (m *model) syncCursorToSelection() {
	if len(m.visible) == 0 {
		m.selected = ""
		return
	}
	for row, i := range m.visible {
		if m.skills[i].Name == m.selected {
			m.tbl.SetCursor(row)
			return
		}
	}
	// The selection is filtered out: adopt whatever the cursor now points at.
	cur := m.tbl.Cursor()
	if cur < 0 || cur >= len(m.visible) {
		cur = 0
		m.tbl.SetCursor(0)
	}
	m.selected = m.skills[m.visible[cur]].Name
}

// selectedSkill returns the currently selected skill and whether one exists.
func (m model) selectedSkill() (DashboardSkill, bool) {
	for _, i := range m.visible {
		if m.skills[i].Name == m.selected {
			return m.skills[i], true
		}
	}
	return DashboardSkill{}, false
}

// showPreview reports whether the preview strip fits this terminal.
func (m model) showPreview() bool {
	return m.height >= previewMinHeight && len(m.visible) > 0
}

// layout recomputes region sizes from the terminal size.
func (m *model) layout() {
	m.tbl.SetColumns(dashColumns(m.width))
	m.tbl.SetWidth(m.width)
	previewH := 0
	if m.showPreview() {
		previewH = m.height / 3
	} else {
		m.focusPreview = false
	}
	// Frame: header (2) + details (1) + footer (1), plus the preview's title
	// and panel border (3) when shown.
	tableH := m.height - previewH - 4
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
	m.refreshPreview()
}

// refreshPreview re-renders the selected skill's SKILL.md into the viewport.
func (m *model) refreshPreview() {
	s, ok := m.selectedSkill()
	if !ok {
		m.vp.SetContent("")
		return
	}
	body, cached := m.previewCache[s.Name]
	if !cached {
		var err error
		if body, err = Preview(s.Markdown, m.vp.Width); err != nil {
			body = Sanitize(s.Markdown)
		}
		m.previewCache[s.Name] = body
	}
	m.vp.SetContent(body)
	m.vp.GotoTop()
}

// Init loads the inventory.
func (m model) Init() tea.Cmd { return m.loadInventory() }

// Update implements tea.Model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		clear(m.previewCache) // cached renders are width-specific
		m.layout()
		return m, nil
	case inventoryMsg:
		clear(m.previewCache) // a refreshed skill's SKILL.md may have changed
		m.setInventory(msg.skills)
		m.layout()
		return m, nil
	case dashErrMsg:
		m.loaded = true
		m.loadErr = msg.err.Error()
		return m, nil
	case planMsg:
		return m.onPlan(msg), nil
	case versionsMsg:
		return m.onVersions(msg), nil
	case applyMsg:
		return m.onApply(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

// onKey dispatches a key by mode. In search mode, printable keys are text
// input and never trigger actions (FR-006); the mutating modes route to their
// own handlers so esc stays contextual (interaction contract).
func (m model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeSearch:
		return m.onSearchKey(msg)
	case modeConfirm:
		return m.onConfirmKey(msg)
	case modeVersionPick:
		return m.onVersionKey(msg)
	case modeBusy:
		return m.onBusyKey(msg)
	case modeResults:
		return m.onResultsKey(msg)
	case modeBrowse:
		return m.onBrowseKey(msg)
	}
	return m.onBrowseKey(msg)
}

// onBrowseKey handles the inventory screen: navigation, search, preview focus,
// the three skill actions, the group update, and quit.
func (m model) onBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", keyCtrlC:
		return m, tea.Quit
	case keyEsc:
		// In browse, esc quits; in the mutating modes it cancels that step.
		return m, tea.Quit
	case "/":
		if len(m.skills) > 0 {
			m.mode = modeSearch
		}
		return m, nil
	case "tab":
		m.focusPreview = !m.focusPreview && m.showPreview()
		return m, nil
	case "u":
		return m.startUpdate()
	case "v":
		return m.startVersionChange()
	case "r":
		return m.startRemove()
	case "g":
		return m.startGroupUpdate()
	}
	if m.focusPreview {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m.moveCursor(msg)
}

// onSearchKey edits the query while search mode is active. esc leaves search,
// enter returns to browse keeping the filter, and typed keys filter the table.
func (m model) onSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		// ctrl+c is the universal interrupt and quits from any mode, unlike a
		// printable key which only edits the query (FR-006).
		return m, tea.Quit
	case keyEsc:
		m.query.value = ""
		m.mode = modeBrowse
		m.recomputeVisible()
		return m, nil
	case keyEnter, keyDown, keyUp:
		m.mode = modeBrowse
		return m.moveCursor(msg)
	}
	if m.query.handleKey(msg) {
		m.recomputeVisible()
	}
	return m, nil
}

// moveCursor forwards a navigation key to the table and tracks the selection
// by identity.
func (m model) moveCursor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := m.tbl.Cursor()
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	if cur := m.tbl.Cursor(); cur != before && cur >= 0 && cur < len(m.visible) {
		m.selected = m.skills[m.visible[cur]].Name
		m.refreshPreview()
	}
	return m, cmd
}

// atEmptyInventory reports whether the inventory is empty while a read-only
// screen owns the view, so the empty-inventory state renders. During an action
// (confirm/version/busy/results) the action screen owns the view even when a
// removal has just emptied the inventory, so its result is not pre-empted.
func (m model) atEmptyInventory() bool {
	return len(m.skills) == 0 && (m.mode == modeBrowse || m.mode == modeSearch)
}

// View implements tea.Model.
func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.st.Accent.Render(" gskill") + m.st.Subtitle.Render(" · installed skills") + "\n\n")

	switch {
	case !m.loaded:
		b.WriteString(m.st.Subtitle.Render("Loading skills ...") + "\n")
		return b.String()
	case m.loadErr != "":
		b.WriteString(m.st.Error.Render("Could not load skills: "+m.loadErr) + "\n")
		b.WriteString(m.st.Hint.Render("q quit") + "\n")
		return b.String()
	case m.atEmptyInventory():
		b.WriteString("No skills installed.\n\n")
		b.WriteString(m.st.Subtitle.Render("Run 'gskill add <source>' to install your first skill.") + "\n")
		b.WriteString(m.st.Hint.Render("q quit") + "\n")
		return b.String()
	}

	switch m.mode {
	case modeConfirm:
		b.WriteString(m.confirmView())
		return b.String()
	case modeVersionPick:
		b.WriteString(m.versionPickView())
		return b.String()
	case modeBusy:
		b.WriteString(m.busyView())
		return b.String()
	case modeResults:
		b.WriteString(m.resultsView())
		return b.String()
	case modeBrowse, modeSearch:
	}

	b.WriteString(m.searchLine())
	if len(m.visible) == 0 {
		b.WriteString(m.st.Warning.Render(fmt.Sprintf("No skills match %q.", strings.TrimSpace(m.query.value))) + "\n")
		b.WriteString(m.st.Hint.Render(" esc clear search · q quit") + "\n")
		return b.String()
	}

	b.WriteString(m.tbl.View() + "\n")
	b.WriteString(m.detailsLine() + "\n")

	if m.showPreview() {
		if s, ok := m.selectedSkill(); ok {
			title := fmt.Sprintf("%s %s", Sanitize(s.Name), Sanitize(s.InstalledVersion))
			b.WriteString(m.st.Accent.Render(" "+title) + "\n")
			panel := m.st.Panel().Width(max(20, m.width-2))
			b.WriteString(panel.Render(m.vp.View()) + "\n")
		}
	}

	b.WriteString(m.footer())
	return b.String()
}

// searchLine renders the active search field, or nothing in browse mode.
func (m model) searchLine() string {
	if m.mode != modeSearch {
		return ""
	}
	return m.st.Accent.Render(" search: ") + Sanitize(m.query.value) + m.st.Cursor.Render("▌") + "\n"
}

// detailsLine renders the selected skill's policy, agents, and the reason it
// cannot update when it is ineligible (FR-004, FR-005, FR-010).
func (m model) detailsLine() string {
	s, ok := m.selectedSkill()
	if !ok {
		return ""
	}
	parts := []string{"policy " + or(s.Policy, "-")}
	if len(s.Agents) > 0 {
		parts = append(parts, "agents "+strings.Join(s.Agents, ","))
	}
	if !s.Updatable() && s.Reason != "" {
		parts = append(parts, s.Reason)
	} else if s.Updatable() && s.Candidate != "" {
		parts = append(parts, "update → "+s.Candidate)
	}
	return m.st.Hint.Render(" " + strings.Join(parts, " · "))
}

// footer renders the key help for the current mode. Browse lists the three
// skill actions and the group update so every action is visible (FR-005); an
// unavailable update's reason is shown in the details line.
func (m model) footer() string {
	pos := fmt.Sprintf("%d/%d", m.tbl.Cursor()+1, len(m.visible))
	hint := " ↑/↓ move · / search · u update · v version · r remove · g group · q quit"
	if m.mode == modeSearch {
		hint = " type to filter · esc clear · enter list"
	}
	return m.st.Hint.Render(hint) + "  " + m.st.Badge.Render(pos) + "\n"
}

// groupOrdered sorts skills into repository-group display order: groups by the
// lowercased repository label then key, and skills by name within a group
// (matching app.groupOrder / spec 032 FR-007).
func groupOrdered(skills []DashboardSkill) []DashboardSkill {
	out := append([]DashboardSkill(nil), skills...)
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].Repo.Label), strings.ToLower(out[j].Repo.Label)
		if li != lj {
			return li < lj
		}
		if out[i].Repo.Key != out[j].Repo.Key {
			return out[i].Repo.Key < out[j].Repo.Key
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// groupsOf derives the contiguous repository groups of an already-ordered
// inventory.
func groupsOf(skills []DashboardSkill) []RepoGroup {
	var groups []RepoGroup
	for _, s := range skills {
		if n := len(groups); n > 0 && groups[n-1].Repo.Equal(s.Repo) {
			groups[n-1].Names = append(groups[n-1].Names, s.Name)
			continue
		}
		groups = append(groups, RepoGroup{Repo: s.Repo, Label: s.Repo.Label, Names: []string{s.Name}})
	}
	return groups
}

// or returns a when non-empty, else fallback.
func or(a, fallback string) string {
	if a == "" {
		return fallback
	}
	return a
}
