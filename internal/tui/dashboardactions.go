package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
)

// The action state machine layered over the browse model: planning, review,
// version picking, apply, and the per-identity result. Every service call runs
// off the UI goroutine as a tea.Cmd so Update never blocks (FR-018), and every
// in-flight action and its result key on skill identity, never a row position
// (FR-017).

// planMsg carries a previewed change back for the review screen. It names the
// identity and kind it was requested for so a result the user has moved past is
// ignored rather than shown against the wrong skill.
type planMsg struct {
	name   string
	kind   ChangeKind
	change ProposedChange
	err    error
}

// versionsMsg carries the selectable versions for the picker.
type versionsMsg struct {
	name     string
	versions []string
	err      error
}

// applyMsg carries the outcomes of an executed action, bound to the identity
// (skill name, or repository key for a group update) it ran against.
type applyMsg struct {
	name     string
	kind     ChangeKind
	outcomes []ActionOutcome
	err      error
}

// planUpdateCmd previews a compatible update off the UI goroutine.
func (m model) planUpdateCmd(name string) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		ch, err := svc.PlanUpdate(ctx, name)
		return planMsg{name: name, kind: ChangeUpdate, change: ch, err: err}
	}
}

// planVersionCmd previews an explicit version change.
func (m model) planVersionCmd(name, target string, latest bool) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		ch, err := svc.PlanVersionChange(ctx, name, target, latest)
		return planMsg{name: name, kind: ChangeVersion, change: ch, err: err}
	}
}

// planRemoveCmd previews a removal and its affected installations.
func (m model) planRemoveCmd(name string) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		ch, err := svc.PlanRemove(ctx, name)
		return planMsg{name: name, kind: ChangeRemove, change: ch, err: err}
	}
}

// versionsCmd lists a skill's selectable versions for the picker.
func (m model) versionsCmd(name string) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		vs, err := svc.Versions(ctx, name)
		return versionsMsg{name: name, versions: vs, err: err}
	}
}

// applyCmd runs the confirmed mutation through the service.
func (m model) applyCmd(p ProposedChange) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		var (
			out []ActionOutcome
			err error
		)
		switch p.Kind {
		case ChangeUpdate:
			out, err = svc.ApplyUpdate(ctx, p.Target)
		case ChangeVersion:
			out, err = svc.ApplyVersionChange(ctx, p.Target, p.Version, p.Latest)
		case ChangeRemove:
			out, err = svc.ApplyRemove(ctx, p.Target)
		case ChangeGroupUpdate:
			out, err = svc.ApplyGroupUpdate(ctx, p.Target)
		}
		return applyMsg{name: p.Target, kind: p.Kind, outcomes: out, err: err}
	}
}

// startUpdate opens the review for a compatible update. It offers the action
// only when the selected skill is updatable; eligibility is never re-derived in
// the view (FR-007). The reason an update is unavailable already shows in the
// details line.
func (m model) startUpdate() (tea.Model, tea.Cmd) {
	s, ok := m.selectedSkill()
	if !ok || !s.Updatable() {
		return m, nil
	}
	m.enterConfirm(s.Name, ChangeUpdate)
	m.actionFrom = s.InstalledVersion
	return m, m.planUpdateCmd(s.Name)
}

// startVersionChange opens the version picker. A local source has no selectable
// versions, so the action is not offered for it (FR-010).
func (m model) startVersionChange() (tea.Model, tea.Cmd) {
	s, ok := m.selectedSkill()
	if !ok || s.Repo.Kind == source.RepoLocal {
		return m, nil
	}
	m.actionName, m.actionKind = s.Name, ChangeVersion
	m.mode = modeVersionPick
	m.verLoading, m.verErr = true, ""
	m.versions, m.verCursor = nil, 0
	m.verInput = lineInput{}
	m.proposed, m.planErr = nil, ""
	m.actionFrom = s.InstalledVersion
	return m, m.versionsCmd(s.Name)
}

// startRemove opens the removal confirmation for the selected skill.
func (m model) startRemove() (tea.Model, tea.Cmd) {
	s, ok := m.selectedSkill()
	if !ok {
		return m, nil
	}
	m.enterConfirm(s.Name, ChangeRemove)
	return m, m.planRemoveCmd(s.Name)
}

// startGroupUpdate opens the review for updating the selected skill's whole
// repository group, reusing spec 032's group-update flow (FR-011). It needs no
// service plan: the members and their candidates come from the inventory, and
// the apply runs through ApplyGroupUpdate.
func (m model) startGroupUpdate() (tea.Model, tea.Cmd) {
	s, ok := m.selectedSkill()
	if !ok {
		return m, nil
	}
	if len(m.groupUpdatable(s.Repo.Key)) == 0 {
		return m, nil
	}
	m.actionName, m.actionKind = s.Repo.Key, ChangeGroupUpdate
	m.planLoading, m.planErr = false, ""
	m.proposed = &ProposedChange{Kind: ChangeGroupUpdate, Target: s.Repo.Key}
	m.mode = modeConfirm
	return m, nil
}

// enterConfirm moves into the review of a single-skill action and clears any
// stale plan from a prior round.
func (m *model) enterConfirm(name string, kind ChangeKind) {
	m.actionName, m.actionKind = name, kind
	m.mode = modeConfirm
	m.planLoading, m.planErr = true, ""
	m.proposed = nil
	m.actionFrom = ""
}

// onPlan records a previewed change, ignoring one the user has moved past.
func (m model) onPlan(msg planMsg) model {
	if m.mode != modeConfirm || msg.name != m.actionName || msg.kind != m.actionKind {
		return m
	}
	m.planLoading = false
	if msg.err != nil {
		m.planErr = msg.err.Error()
		return m
	}
	ch := msg.change
	m.proposed = &ch
	return m
}

// onVersions records the picker's candidates for the active version change.
func (m model) onVersions(msg versionsMsg) model {
	if m.mode != modeVersionPick || msg.name != m.actionName {
		return m
	}
	m.verLoading = false
	if msg.err != nil {
		m.verErr = msg.err.Error()
		return m
	}
	m.versions, m.verCursor = msg.versions, 0
	return m
}

// onApply records the result of the in-flight action, keyed by identity, then
// refreshes the inventory in place (FR-012, FR-016). A failed or refused
// outcome (or an apply error) sets the session's partial-failure exit
// (FR-025); a clean or no-change run leaves it alone.
func (m model) onApply(msg applyMsg) (tea.Model, tea.Cmd) {
	if msg.name != m.inflight {
		return m, nil
	}
	m.inflight = ""
	m.results = msg.outcomes
	m.resultErr = ""
	if msg.err != nil && len(msg.outcomes) == 0 {
		m.resultErr = msg.err.Error()
	}
	if applyFailed(msg) {
		m.sessionErr = fmt.Errorf("%w: %s failed for %s", errs.ErrPartialInstall, msg.kind, msg.name)
	}
	m.mode = modeResults
	return m, m.loadInventory()
}

// applyFailed reports whether an apply result should mark the session failed.
func applyFailed(msg applyMsg) bool {
	if msg.err != nil {
		return true
	}
	for _, o := range msg.outcomes {
		if o.Result == "failed" || o.Result == "refused" {
			return true
		}
	}
	return false
}

// onConfirmKey handles the review screen: enter/y confirms, esc/q/n cancels,
// ctrl+c interrupts.
func (m model) onConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		return m.interrupt()
	case keyEsc, "q", "n":
		return m.cancelAction(), nil
	case keyEnter, "y":
		return m.confirmAction()
	}
	return m, nil
}

// confirmAction applies the reviewed change after a final revalidation against
// current state (FR-019). Nothing is written before this edge (FR-009).
func (m model) confirmAction() (tea.Model, tea.Cmd) {
	if m.proposed == nil || m.planLoading || m.planErr != "" || m.inflight != "" {
		return m, nil
	}
	p := *m.proposed
	if reason, stale := m.staleReason(p); stale {
		m.results = []ActionOutcome{{Name: p.Target, Kind: p.Kind, Result: "refused", Detail: reason}}
		m.resultErr = ""
		m.sessionErr = fmt.Errorf("%w: %s refused for %s", errs.ErrPartialInstall, p.Kind, p.Target)
		m.mode = modeResults
		return m, nil
	}
	m.inflight = p.Target
	m.mode = modeBusy
	return m, m.applyCmd(p)
}

// staleReason revalidates the proposed change against the current inventory and
// returns why it may no longer apply, so a stale transition is refused rather
// than run (FR-019).
func (m model) staleReason(p ProposedChange) (string, bool) {
	if p.Kind == ChangeGroupUpdate {
		if len(m.groupUpdatable(p.Target)) == 0 {
			return "no skills in this group can be updated now", true
		}
		return "", false
	}
	s, ok := m.skillByName(p.Target)
	if !ok {
		return "skill is no longer installed", true
	}
	switch p.Kind {
	case ChangeUpdate:
		if !s.Updatable() {
			return "skill is no longer updatable", true
		}
		fallthrough
	case ChangeVersion:
		if m.actionFrom != "" && s.InstalledVersion != m.actionFrom {
			return "installed version changed since the preview", true
		}
	case ChangeRemove, ChangeGroupUpdate:
	}
	return "", false
}

// cancelAction returns to browse writing nothing (FR-009).
func (m model) cancelAction() model {
	m.mode = modeBrowse
	m.proposed = nil
	m.actionName, m.actionKind = "", ""
	m.actionFrom = ""
	m.planLoading, m.planErr = false, ""
	return m
}

// onVersionKey edits the picker: arrows move the candidate cursor, printable
// keys type an explicit target, enter chooses, esc clears then cancels. Letters
// stay text input so a tag like "v2" types cleanly.
func (m model) onVersionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		return m.interrupt()
	case keyEsc:
		if m.verInput.value != "" {
			m.verInput.value = ""
			return m, nil
		}
		return m.cancelAction(), nil
	case keyUp:
		if m.verCursor > 0 {
			m.verCursor--
		}
		return m, nil
	case keyDown:
		if m.verCursor < len(m.versions) {
			m.verCursor++
		}
		return m, nil
	case keyEnter:
		return m.pickVersion()
	}
	m.verInput.handleKey(msg)
	return m, nil
}

// pickVersion resolves the chosen target (typed target, the "latest" row, or a
// listed version) and opens the review for it.
func (m model) pickVersion() (tea.Model, tea.Cmd) {
	if m.verLoading {
		return m, nil
	}
	var (
		target string
		latest bool
	)
	switch {
	case strings.TrimSpace(m.verInput.value) != "":
		target = strings.TrimSpace(m.verInput.value)
	case m.verCursor == 0:
		latest = true
	default:
		target = m.versions[m.verCursor-1]
	}
	name := m.actionName
	m.mode = modeConfirm
	m.planLoading, m.planErr, m.proposed = true, "", nil
	return m, m.planVersionCmd(name, target, latest)
}

// onBusyKey ignores action keys while an apply runs (FR-018); only ctrl+c
// interrupts.
func (m model) onBusyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == keyCtrlC {
		return m.interrupt()
	}
	return m, nil
}

// onResultsKey dismisses the results and returns to the refreshed inventory.
func (m model) onResultsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == keyCtrlC {
		return m.interrupt()
	}
	return m.dismissResults(), nil
}

// dismissResults clears the finished action and returns to browse. The
// inventory was already refreshed when the result arrived.
func (m model) dismissResults() model {
	m.mode = modeBrowse
	m.results, m.resultErr = nil, ""
	m.proposed = nil
	m.actionName, m.actionKind = "", ""
	m.actionFrom = ""
	return m
}

// interrupt quits with the cancellation exit (code 130), like the other TUI
// models' ctrl+c handling (FR-025).
func (m model) interrupt() (tea.Model, tea.Cmd) {
	m.sessionErr = errs.ErrCancelled
	return m, tea.Quit
}

// skillByName finds an installed skill by its identity.
func (m model) skillByName(name string) (DashboardSkill, bool) {
	for _, s := range m.skills {
		if s.Name == name {
			return s, true
		}
	}
	return DashboardSkill{}, false
}

// groupUpdatable lists the skills in a repository group a compatible update can
// move now, in display order.
func (m model) groupUpdatable(repoKey string) []DashboardSkill {
	var out []DashboardSkill
	for _, s := range m.skills {
		if s.Repo.Key == repoKey && s.Updatable() {
			out = append(out, s)
		}
	}
	return out
}

// confirmView renders the single-confirmation review for the pending action.
func (m model) confirmView() string {
	var b strings.Builder
	if m.planLoading {
		b.WriteString(m.st.Subtitle.Render(" Planning ...") + "\n")
		b.WriteString(m.st.Hint.Render(" esc cancel") + "\n")
		return b.String()
	}
	if m.planErr != "" {
		b.WriteString(m.st.Error.Render(" Cannot plan this action: "+Sanitize(m.planErr)) + "\n\n")
		b.WriteString(m.st.Hint.Render(" esc back") + "\n")
		return b.String()
	}
	if m.proposed == nil {
		return m.st.Hint.Render(" esc back") + "\n"
	}
	p := *m.proposed
	switch p.Kind {
	case ChangeRemove:
		b.WriteString(m.removeConfirm(p))
	case ChangeGroupUpdate:
		b.WriteString(m.groupConfirm(p))
	case ChangeUpdate, ChangeVersion:
		b.WriteString(m.revisionConfirm(p))
	}
	b.WriteString("\n" + m.st.Hint.Render(" enter confirm · esc cancel") + "\n")
	return b.String()
}

// revisionConfirm renders an update or version change: installed -> target and,
// when skills.toml changes, the declaration before and after (FR-009).
func (m model) revisionConfirm(p ProposedChange) string {
	var b strings.Builder
	verb := "Update"
	if p.Kind == ChangeVersion {
		verb = "Change version of"
	}
	b.WriteString(m.st.Accent.Render(" "+verb+" "+Sanitize(p.Target)) + "\n\n")
	b.WriteString("   " + Sanitize(or(p.FromRev, "?")) + " → " + Sanitize(or(p.ToRev, "?")) + "\n")
	if p.DeclBefore != "" || p.DeclAfter != "" {
		b.WriteString("\n" + m.st.Subtitle.Render(" skills.toml") + "\n")
		b.WriteString("   " + m.st.Error.Render("- "+Sanitize(p.DeclBefore)) + "\n")
		b.WriteString("   " + m.st.Success.Render("+ "+Sanitize(p.DeclAfter)) + "\n")
	}
	return b.String()
}

// removeConfirm names the skill, its source repository, and the installations a
// removal affects before anything is deleted (FR-013, FR-014).
func (m model) removeConfirm(p ProposedChange) string {
	var b strings.Builder
	b.WriteString(m.st.Accent.Render(" Remove "+Sanitize(p.Target)) + "\n\n")
	if s, ok := m.skillByName(p.Target); ok {
		b.WriteString("   source   " + Sanitize(s.SourceLabel) + "\n")
	}
	if p.FromRev != "" {
		b.WriteString("   version  " + Sanitize(p.FromRev) + "\n")
	}
	if len(p.Installations) > 0 {
		b.WriteString("   affects  " + Sanitize(p.Installations[0]) + "\n")
		for _, ins := range p.Installations[1:] {
			b.WriteString("            " + Sanitize(ins) + "\n")
		}
	}
	b.WriteString("\n" + m.st.Subtitle.Render(
		" Removal updates skills.toml and skills-lock.json and deletes the managed installed content.") + "\n")
	return b.String()
}

// groupConfirm lists the group's updatable skills, each moving within its own
// declaration (FR-011), reusing the update table's transition presentation.
func (m model) groupConfirm(p ProposedChange) string {
	var b strings.Builder
	label := p.Target
	members := m.groupUpdatable(p.Target)
	if len(members) > 0 {
		label = members[0].SourceLabel
	}
	b.WriteString(m.st.Accent.Render(" Update group "+Sanitize(label)) + "\n\n")
	for _, s := range members {
		row := TableRow{Current: s.InstalledVersion, Candidate: s.Candidate}
		b.WriteString("   " + Sanitize(s.Name) + "  " + Sanitize(transition(row)) + "\n")
	}
	return b.String()
}

// versionPickView renders the version picker: a "latest" row, the candidate
// versions, and a typed-target field.
func (m model) versionPickView() string {
	var b strings.Builder
	b.WriteString(m.st.Accent.Render(" Change version of "+Sanitize(m.actionName)) + "\n\n")
	if m.verLoading {
		b.WriteString(m.st.Subtitle.Render(" Loading versions ...") + "\n")
		b.WriteString("\n" + m.st.Hint.Render(" esc cancel") + "\n")
		return b.String()
	}
	if m.verErr != "" {
		b.WriteString(m.st.Warning.Render(" Could not list versions: "+Sanitize(m.verErr)) + "\n")
	}
	typing := strings.TrimSpace(m.verInput.value) != ""
	for i, label := range append([]string{"latest"}, m.versions...) {
		cursor := "  "
		if i == m.verCursor && !typing {
			cursor = m.st.Cursor.Render("❯") + " "
		}
		b.WriteString(cursor + Sanitize(label) + "\n")
	}
	b.WriteString("\n or type a target: " + Sanitize(m.verInput.value) + m.st.Cursor.Render("▌") + "\n")
	b.WriteString("\n" + m.st.Hint.Render(" ↑/↓ pick · type target · enter choose · esc cancel") + "\n")
	return b.String()
}

// busyView shows the in-flight action; its keys are ignored (FR-018).
func (m model) busyView() string {
	var b strings.Builder
	b.WriteString(m.st.Subtitle.Render(" "+actionGerund(m.actionKind)+" "+Sanitize(m.inflight)+" ...") + "\n")
	b.WriteString("\n" + m.st.Hint.Render(" working") + "\n")
	return b.String()
}

// resultsView lists each skill's outcome, bound to its identity, including a
// partial success; a failure keeps the session alive (FR-020).
func (m model) resultsView() string {
	var b strings.Builder
	b.WriteString(m.st.Accent.Render(" Results") + "\n\n")
	if m.resultErr != "" {
		b.WriteString("   " + m.st.Error.Render(Sanitize(m.resultErr)) + "\n")
	}
	for _, o := range m.results {
		style := m.st.Success
		if o.Result == "failed" || o.Result == "refused" {
			style = m.st.Error
		}
		line := "   " + Sanitize(o.Name) + "  " + style.Render(Sanitize(o.Result))
		if o.Detail != "" {
			line += m.st.Hint.Render("  " + Sanitize(o.Detail))
		}
		b.WriteString(line + "\n")
	}
	if len(m.results) == 0 && m.resultErr == "" {
		b.WriteString("   " + m.st.Subtitle.Render("no changes") + "\n")
	}
	b.WriteString("\n" + m.st.Hint.Render(" press any key to continue") + "\n")
	return b.String()
}

// actionGerund names an action in progress for the busy screen.
func actionGerund(k ChangeKind) string {
	switch k {
	case ChangeRemove:
		return "Removing"
	case ChangeVersion:
		return "Changing version of"
	case ChangeGroupUpdate:
		return "Updating group"
	case ChangeUpdate:
		return "Updating"
	default:
		return "Working on"
	}
}
