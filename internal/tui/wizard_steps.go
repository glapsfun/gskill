package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
)

// Per-step key handling and views for the onboarding wizard (spec 011 US1–US5).
// Steps get first refusal on keys via stepKey; unhandled keys fall back to the
// shell (q/ctrl+c cancel, esc/b back).

// stepKey dispatches a key to the current step.
func (m wizardModel) stepKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	switch m.step {
	case stepSource:
		return m.sourceKey(key)
	case stepWelcome:
		return m.welcomeKey(key)
	case stepSelect:
		return m.selectKey(key)
	case stepVersion:
		return m.versionKey(key)
	case stepAgents:
		return m.agentsKey(key)
	case stepPreview:
		return m.previewKey(key)
	case stepProgress, stepSummary:
		return m, nil, false
	default:
		return m, nil, false
	}
}

// hintLine renders the keyboard hint footer for a step (FR-006).
func (m wizardModel) hintLine(hints string) string {
	return "\n" + m.st.Hint.Render(hints) + "\n"
}

// header renders the step title line with a position badge.
func (m wizardModel) header(title string) string {
	return m.st.Title.Render(title) + "  " + m.st.Badge.Render("["+m.step.String()+"]") + "\n\n"
}

// ---- welcome ----------------------------------------------------------------

func (m wizardModel) welcomeKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	if key.String() != keyEnter {
		return m, nil, false
	}
	if m.discovering {
		return m, nil, true // still resolving; ignore until discovery lands
	}
	next, cmd := m.goForward()
	return next, cmd, true
}

func (m wizardModel) viewWelcome() string {
	var b strings.Builder
	b.WriteString(m.header("Welcome to gskill onboarding"))
	fmt.Fprintf(&b, "Source: %s\n\n", m.st.Accent.Render(Sanitize(m.session.Source)))

	if m.discovering {
		b.WriteString("⏳ Resolving source and discovering skills…\n")
		b.WriteString(m.hintLine("q cancel"))
		return b.String()
	}

	valid, invalid := 0, 0
	for _, s := range m.disc.Skills {
		if s.Valid {
			valid++
		} else {
			invalid++
		}
	}
	fmt.Fprintf(&b, "Discovered %s installable skill(s)", m.st.Accent.Render(strconv.Itoa(valid)))
	if invalid > 0 {
		fmt.Fprintf(&b, " (%d invalid)", invalid)
	}
	b.WriteString(".\n")
	m.writeWelcomeDetection(&b)
	b.WriteString("\n")
	b.WriteString(m.st.Subtitle.Render("This guided flow will walk you through:") + "\n")
	b.WriteString("  1. selecting skills   2. choosing a version   3. picking target agents\n")
	b.WriteString("  4. reviewing the plan  5. approving            6. installing\n")
	b.WriteString("\nNothing is written until you approve the plan.\n")
	for _, w := range m.disc.Warnings {
		b.WriteString(m.st.Warning.Render("warning: "+Sanitize(w)) + "\n")
	}
	b.WriteString(m.hintLine("enter continue · q cancel"))
	return b.String()
}

// writeWelcomeDetection reports detected agents and available versions on the
// welcome step (FR-005, US1/AC1). Data still loading renders as such; steps
// answered by flags (whose listings were never requested) are simply omitted.
func (m wizardModel) writeWelcomeDetection(b *strings.Builder) {
	switch {
	case m.agentsLoading:
		b.WriteString(m.st.Subtitle.Render("Agents:   detecting…") + "\n")
	case len(m.agentChoices) > 0:
		detected := make([]string, 0, len(m.agentChoices))
		for _, c := range m.agentChoices {
			if c.Detected {
				detected = append(detected, c.DisplayName)
			}
		}
		line := fmt.Sprintf("Agents:   %d detected of %d supported", len(detected), len(m.agentChoices))
		if len(detected) > 0 {
			line += " (" + strings.Join(detected, ", ") + ")"
		}
		b.WriteString(line + "\n")
	}

	switch {
	case m.versionsLoading:
		b.WriteString(m.st.Subtitle.Render("Versions: listing…") + "\n")
	case m.versions.Degraded:
		b.WriteString(m.st.Warning.Render("Versions: browsing unavailable ("+Sanitize(m.versions.DegradedReason)+")") + "\n")
	case len(m.versions.Candidates) > 0:
		releases, branches := 0, 0
		for _, c := range m.versions.Candidates {
			switch c.Kind {
			case app.VersionRelease:
				releases++
			case app.VersionBranch:
				branches++
			}
		}
		fmt.Fprintf(b, "Versions: %d release(s), %d branch(es) available\n", releases, branches)
	}
}

// ---- skill selection (reuses the spec-009 selector) --------------------------

func (m wizardModel) selectKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	switch key.String() {
	case keyEnter:
		return m.confirmSelection()
	case keyEsc:
		return m.selectEsc()
	case keyUp:
		m.sel.moveUp()
		return m, nil, true
	case keyDown:
		m.sel.moveDown()
		return m, nil, true
	case keyCtrlC:
		return m, nil, false
	}

	if m.sel.filtering {
		next, _ := m.sel.handleFilterKey(key)
		if sm, ok := next.(selectorModel); ok {
			m.sel = sm
		}
		return m, nil, true
	}
	return m.selectNavKey(key)
}

// confirmSelection commits the toggled skills into the session (≥1 required).
func (m wizardModel) confirmSelection() (wizardModel, tea.Cmd, bool) {
	idx := m.sel.chosenIndices()
	if len(idx) == 0 {
		m.selErr = "select at least one skill (space toggles)"
		return m, nil, true
	}
	m.selErr = ""
	// A fresh slice, never a truncate-and-reuse: an in-flight plan snapshot
	// may still be reading the previous backing array (review finding).
	selected := make([]discovery.DiscoveredSkill, 0, len(idx))
	for _, i := range idx {
		selected = append(selected, m.disc.Skills[i])
	}
	m.session.Selected = selected
	next, cmd := m.goForward()
	return next, cmd, true
}

// selectEsc applies the selector's two-stage esc (unfocus filter, then clear
// query); with neither active it defers to the shell's back-navigation.
func (m wizardModel) selectEsc() (wizardModel, tea.Cmd, bool) {
	if m.sel.filtering {
		m.sel.filtering = false
		return m, nil, true
	}
	if m.sel.filter.value != "" {
		m.sel.filter.value = ""
		m.sel.recomputeVisible()
		return m, nil, true
	}
	return m, nil, false
}

// selectNavKey handles the selector's navigation shortcuts outside filtering.
func (m wizardModel) selectNavKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	switch key.String() {
	case "k":
		m.sel.moveUp()
	case "j":
		m.sel.moveDown()
	case " ", "x":
		m.sel.toggle()
		m.selErr = ""
	case "/":
		m.sel.filtering = true
	default:
		return m, nil, false
	}
	return m, nil, true
}

func (m wizardModel) viewSelect() string {
	var b strings.Builder
	b.WriteString(m.header("Select skills to install"))
	// The selector renders its own filter line and bounded window; the wizard
	// frames it with its own header, position badge, and hint footer.
	b.WriteString(m.sel.viewBody())
	b.WriteString("\n" + m.st.Badge.Render(m.sel.position()) + "\n")
	if m.selErr != "" {
		b.WriteString(m.st.Error.Render(m.selErr) + "\n")
	}
	b.WriteString(m.hintLine("↑/↓ move · space toggle · / filter · enter continue · esc back · q cancel"))
	return b.String()
}

// ---- version (US3) -----------------------------------------------------------

func (m *wizardModel) startVersions() tea.Cmd {
	m.versionsLoading = true
	return m.versionsCmd()
}

// versionsCmd is the flag-free version-listing command builder.
func (m wizardModel) versionsCmd() tea.Cmd {
	versions := m.phases.Versions
	ctx := m.ctx
	gen := m.sourceGen
	return func() tea.Msg {
		res, err := versions(ctx)
		return versionsDoneMsg{res: res, err: err, gen: gen}
	}
}

func (m wizardModel) onVersionsDone(msg versionsDoneMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.sourceGen {
		return m, nil // listing from an abandoned source: drop
	}
	m.versionsLoading = false
	if msg.err != nil {
		// Version listing is never fatal (FR-012): degrade in place.
		m.versions = app.VersionList{Degraded: true, DegradedReason: msg.err.Error()}
	} else {
		m.versions = msg.res
	}
	// A shrunken listing must never leave the cursor past the typed-ref row.
	if m.versionCursor > len(m.versions.Candidates) {
		m.versionCursor = len(m.versions.Candidates)
	}
	return m, nil
}

func (m wizardModel) versionKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	if m.versionTyping {
		return m.versionTypedKey(key)
	}
	switch key.String() {
	case keyUp, "k":
		if m.versionCursor > 0 {
			m.versionCursor--
		}
		return m, nil, true
	case keyDown, "j":
		// The row after the last candidate is the typed-exact-ref entry.
		if m.versionCursor < len(m.versions.Candidates) {
			m.versionCursor++
		}
		return m, nil, true
	case keyEnter:
		if m.versionCursor == len(m.versions.Candidates) {
			m.versionTyping = true
			return m, nil, true
		}
		m.applyVersionChoice()
		next, cmd := m.goForward()
		return next, cmd, true
	}
	return m, nil, false
}

// versionTypedKey edits and applies a typed exact ref or commit (FR-012): a
// full 40-hex value pins a commit, anything else is requested as a ref.
func (m wizardModel) versionTypedKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	switch key.Type { //nolint:exhaustive // enter/esc here; editing is the shared lineInput
	case tea.KeyEnter:
		value := strings.TrimSpace(m.versionInput.value)
		if value == "" {
			m.versionTyping = false
			return m, nil, true
		}
		m.session.Version, m.session.RefSpec, m.session.Commit = "", "", ""
		if isFullSHA(value) {
			m.session.Commit = value
		} else {
			m.session.RefSpec = value
		}
		m.session.VersionLabel = value
		// Leave input mode so re-entering the step navigates the list again
		// instead of resuming a stale buffer (review finding).
		m.versionTyping = false
		m.versionInput = newLineInput()
		next, cmd := m.goForward()
		return next, cmd, true
	case tea.KeyEsc:
		m.versionTyping = false
		return m, nil, true
	default:
		m.versionInput.handleKey(key)
		return m, nil, true
	}
}

// isFullSHA reports whether s looks like a full 40-hex commit SHA.
func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// applyVersionChoice writes the highlighted candidate into the session's
// requested pin (FR-012/FR-013); "latest" keeps default resolution.
func (m *wizardModel) applyVersionChoice() {
	if len(m.versions.Candidates) == 0 {
		m.session.VersionLabel = "latest"
		return
	}
	c := m.versions.Candidates[m.versionCursor]
	m.session.Version, m.session.RefSpec, m.session.Commit = "", "", ""
	m.session.VersionLabel = c.Label
	switch c.Kind {
	case app.VersionLatest:
		// Default resolution; no pin.
	case app.VersionRelease, app.VersionBranch:
		m.session.RefSpec = c.Ref
	case app.VersionCommit:
		m.session.Commit = c.Commit
	}
}

func (m wizardModel) viewVersion() string {
	var b strings.Builder
	b.WriteString(m.header("Choose a version"))
	if m.versionsLoading {
		b.WriteString("⏳ Listing releases and branches…\n")
		b.WriteString(m.hintLine("q cancel"))
		return b.String()
	}
	if m.versions.Degraded {
		b.WriteString(m.st.Warning.Render("⚠ version browsing unavailable: "+Sanitize(m.versions.DegradedReason)) + "\n")
		b.WriteString(m.st.Subtitle.Render("\"latest\" will be used; a branch, tag, or commit can still be set with --ref/--commit.") + "\n\n")
	}
	rows := make([]string, 0, len(m.versions.Candidates)+1)
	for i, c := range m.versions.Candidates {
		cursor := "  "
		label := Sanitize(c.Label)
		if i == m.versionCursor {
			cursor = m.st.Cursor.Render("❯") + " "
			label = m.st.Selected.Render(label)
		}
		row := cursor + label
		if c.Metadata != "" {
			row += "  " + m.st.Subtitle.Render(Sanitize(c.Metadata))
		}
		rows = append(rows, row)
	}

	// Synthetic last row: type an exact ref or commit (FR-012).
	typedCursor := "  "
	typedLabel := "type an exact ref or commit…"
	if m.versionCursor == len(m.versions.Candidates) {
		typedCursor = m.st.Cursor.Render("❯") + " "
		typedLabel = m.st.Selected.Render(typedLabel)
	}
	if m.versionTyping {
		fmt.Fprintf(&b, "%s%s %s█\n", typedCursor, m.st.Selected.Render("ref:"), Sanitize(m.versionInput.value))
		b.WriteString(m.hintLine("enter apply · esc cancel input"))
		return b.String()
	}
	rows = append(rows, typedCursor+typedLabel)

	for _, line := range m.cursorWindow(rows, m.versionCursor) {
		b.WriteString(line + "\n")
	}
	b.WriteString(m.hintLine("↑/↓ move · enter choose · esc back · q cancel"))
	return b.String()
}

// cursorWindow bounds rows to the terminal height, keeping the cursor row
// visible, so long version lists never overflow a small terminal (FR-022).
func (m wizardModel) cursorWindow(rows []string, cursor int) []string {
	page := pageFor(m.height, wizardReservedRows)
	return windowRows(rows, page, cursorOffset(cursor, page, len(rows)), m.st.Hint)
}

// ---- agents (US2) ------------------------------------------------------------

func (m *wizardModel) startAgents() tea.Cmd {
	m.agentsLoading = true
	return m.agentsCmd()
}

// agentsCmd is the flag-free agent-listing command builder.
func (m wizardModel) agentsCmd() tea.Cmd {
	agents := m.phases.Agents
	ctx := m.ctx
	return func() tea.Msg {
		choices, err := agents(ctx)
		return agentsDoneMsg{choices: choices, err: err}
	}
}

func (m wizardModel) onAgentsDone(msg agentsDoneMsg) (tea.Model, tea.Cmd) {
	m.agentsLoading = false
	if msg.err != nil {
		m.failed = msg.err
		return m, nil
	}
	m.agentChoices = msg.choices
	for i, c := range m.agentChoices {
		if c.Preselected {
			m.agentChosen[i] = true
		}
	}
	return m, nil
}

func (m wizardModel) agentsKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	switch key.String() {
	case keyUp, "k":
		if m.agentCursor > 0 {
			m.agentCursor--
		}
		return m, nil, true
	case keyDown, "j":
		if m.agentCursor < len(m.agentChoices)-1 {
			m.agentCursor++
		}
		return m, nil, true
	case " ", "x":
		m.agentChosen[m.agentCursor] = !m.agentChosen[m.agentCursor]
		m.agentErr = ""
		return m, nil, true
	case keyEnter:
		var ids []string
		for i, c := range m.agentChoices {
			if m.agentChosen[i] {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) == 0 {
			m.agentErr = "select at least one agent (space toggles)"
			return m, nil, true
		}
		m.agentErr = ""
		m.session.AgentIDs = ids
		next, cmd := m.goForward()
		return next, cmd, true
	}
	return m, nil, false
}

func (m wizardModel) viewAgents() string {
	var b strings.Builder
	b.WriteString(m.header("Choose target agents"))
	if m.agentsLoading {
		b.WriteString("⏳ Detecting agents…\n")
		b.WriteString(m.hintLine("q cancel"))
		return b.String()
	}
	for i, c := range m.agentChoices {
		cursor := "  "
		if i == m.agentCursor {
			cursor = m.st.Cursor.Render("❯") + " "
		}
		check := "[ ]"
		if m.agentChosen[i] {
			check = m.st.Selected.Render("[x]")
		}
		name := c.DisplayName
		if i == m.agentCursor {
			name = m.st.Selected.Render(name)
		}
		b.WriteString(cursor + check + " " + name)
		if c.Detected {
			b.WriteString("  " + m.st.Success.Render("(detected)"))
		}
		b.WriteString("\n")
	}
	if m.agentErr != "" {
		b.WriteString(m.st.Error.Render(m.agentErr) + "\n")
	}
	b.WriteString(m.hintLine("↑/↓ move · space toggle · enter continue · esc back · q cancel"))
	return b.String()
}

// ---- preview & approval (FR-015..FR-017) -------------------------------------

func (m wizardModel) previewKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	switch key.String() {
	case keyEnter, "a":
		if !m.planReady {
			return m, nil, true
		}
		if len(m.plan.Conflicts) > 0 {
			return m, nil, true // approval blocked while conflicts exist (FR-016)
		}
		next, cmd := m.approve()
		return next, cmd, true
	case keyUp, "k":
		if m.previewOffset > 0 {
			m.previewOffset--
		}
		return m, nil, true
	case keyDown, "j":
		m.previewOffset++ // clamped against the body length at render time
		return m, nil, true
	}
	return m, nil, false
}

func (m wizardModel) viewPreview() string {
	var b strings.Builder
	b.WriteString(m.header("Review the installation plan"))
	if m.planning || !m.planReady {
		b.WriteString("⏳ Computing installation plan…\n")
		b.WriteString(m.hintLine("esc back · q cancel"))
		return b.String()
	}

	body := m.previewBody()
	for _, line := range m.windowLines(body) {
		b.WriteString(line + "\n")
	}

	if len(m.plan.Conflicts) > 0 {
		b.WriteString(m.hintLine("↑/↓ scroll · esc/b go back and edit · q cancel"))
		return b.String()
	}
	b.WriteString(m.hintLine("enter approve & install · ↑/↓ scroll · esc/b go back and edit · q cancel"))
	return b.String()
}

// previewBody renders the full plan as styled lines from the shared
// app.InstallPlan.Lines sequence — the same content `add --dry-run` prints —
// so the wizard preview and the scripted plan cannot drift (FR-015/FR-024).
func (m wizardModel) previewBody() []string {
	var lines []string
	sawConflict := false
	for _, pl := range m.plan.Lines(m.versionDisplay()) {
		text := Sanitize(pl.Text)
		switch pl.Kind {
		case app.PlanLineMeta:
			lines = append(lines, m.st.Accent.Render(text))
		case app.PlanLineInit:
			lines = append(lines, m.st.Warning.Render("• "+text))
		case app.PlanLineAgent:
			lines = append(lines, m.st.Subtitle.Render(text))
		case app.PlanLineAction:
			lines = append(lines, "  + "+text)
		case app.PlanLineFileOp:
			lines = append(lines, "      "+text)
		case app.PlanLineWarning:
			lines = append(lines, m.st.Warning.Render("warning: "+text))
		case app.PlanLineConflict:
			if !sawConflict {
				sawConflict = true
				lines = append(lines, "", m.st.Error.Render("Conflicts block approval:"))
			}
			lines = append(lines, "  "+m.st.Error.Render("✗")+" "+text)
		}
	}
	return lines
}

// wizardReservedRows is the frame around a wizard step's scrollable window:
// header (2), the two more-markers, and the hint footer (2). It governs the
// preview and the version list alike.
const wizardReservedRows = 6

// windowLines bounds body to the terminal height at the free-scroll preview
// offset, so small terminals stay readable (FR-022, SC at 80×24).
func (m wizardModel) windowLines(body []string) []string {
	return windowRows(body, pageFor(m.height, wizardReservedRows), m.previewOffset, m.st.Hint)
}

// versionDisplay renders the chosen version for the preview and summary.
func (m wizardModel) versionDisplay() string {
	if m.session.VersionLabel != "" {
		return m.session.VersionLabel
	}
	rev := m.plan.Revision
	switch {
	case rev.Version != "":
		return rev.Version
	case rev.Tag != "":
		return rev.Tag
	case rev.Branch != "":
		return rev.Branch
	case rev.Commit != "":
		return shortCommit(rev.Commit)
	default:
		return "latest"
	}
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// ---- progress ------------------------------------------------------------------

func (m wizardModel) viewProgress() string {
	var b strings.Builder
	b.WriteString(m.header("Installing"))
	done := map[string]bool{}
	for _, e := range m.events {
		if e.Stage == "record" {
			done[e.Skill] = true
		}
	}
	for _, s := range m.session.Selected {
		mark := "…"
		if done[s.ID] {
			mark = m.st.Success.Render("✓")
		}
		fmt.Fprintf(&b, "  %s %s\n", mark, Sanitize(s.ID))
	}
	b.WriteString(m.hintLine("installing — please wait"))
	return b.String()
}

// ---- summary (FR-021) -----------------------------------------------------------

func (m wizardModel) viewSummary() string {
	var b strings.Builder
	b.WriteString(m.header("Installed successfully"))
	fmt.Fprintf(&b, "%s\n\n", m.st.Success.Render(fmt.Sprintf("✓ %d skill(s) installed", len(m.result.Installed))))
	for _, s := range m.result.Installed {
		fmt.Fprintf(&b, "  %s %s %s\n", m.st.Success.Render("✓"), Sanitize(s.Name), m.st.Subtitle.Render("("+Sanitize(m.versionDisplay())+")"))
		targets := make([]string, 0, len(s.Targets))
		for id := range s.Targets {
			targets = append(targets, id)
		}
		sort.Strings(targets)
		for _, id := range targets {
			fmt.Fprintf(&b, "      %s → %s\n", id, Sanitize(s.Targets[id]))
		}
	}
	for _, w := range m.result.Warnings {
		b.WriteString("  " + m.st.Warning.Render("warning: "+Sanitize(w)) + "\n")
	}
	b.WriteString("\n" + m.st.Subtitle.Render("Next steps:") + "\n")
	b.WriteString("  gskill list      view installed skills\n")
	b.WriteString("  gskill status    check per-agent health\n")
	b.WriteString("  gskill update    advance versions later\n")
	b.WriteString("  gskill remove    uninstall a skill\n")
	b.WriteString(m.hintLine("enter/q exit"))
	return b.String()
}

// ---- errors -----------------------------------------------------------------------

func (m wizardModel) viewError() string {
	var b strings.Builder
	b.WriteString(m.st.Error.Render("✗ "+Sanitize(m.failed.Error())) + "\n")
	if errors := m.failedHint(); errors != "" {
		b.WriteString(m.st.Hint.Render("→ "+errors) + "\n")
	}
	b.WriteString(m.hintLine("press any key to exit"))
	return b.String()
}

// failedHint surfaces the actionable hint carried by a coded error, if any.
func (m wizardModel) failedHint() string {
	return errs.HintOf(m.failed)
}
