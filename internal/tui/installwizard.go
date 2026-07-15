package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// Lock-install wizard (spec 012 US1, FR-013): the interactive flow for
// `gskill install` on a project with a skills-lock.json. It reuses the
// onboarding wizard's theme and huh form patterns with a shorter step chain:
// agents (with the lock summary) → preview → progress → summary.

// LockWizardSkill is one lock entry shown in the flow.
type LockWizardSkill struct {
	Name   string
	Source string
	// Agents is the entry's currently recorded gskill.agents (nil for a raw,
	// unmanaged entry), used to compute the kept/added/removed plan the
	// preview screen shows before the user confirms (spec 013 FR-006).
	Agents []string
}

// LockWizardPhases are the app-layer use-cases the flow drives, injected by
// the CLI so the wizard stays view-pure and unit-testable. Execute receives
// the wizard's progress callback so the run streams install lifecycle events
// into the live progress view (spec 014 US1).
type LockWizardPhases struct {
	Agents  func(context.Context) ([]app.AgentChoice, error)
	Execute func(context.Context, []string, func(app.InstallProgressEvent)) (app.InstallFromLockResult, error)
}

// LockWizardConfig configures a lock-install wizard run.
type LockWizardConfig struct {
	LockPath string
	Skills   []LockWizardSkill
	Phases   LockWizardPhases
}

// LockWizardOutcome is what a finished flow reports back to the CLI.
type LockWizardOutcome struct {
	Cancelled bool // user quit before approving: zero writes (CodeCancelled)
	NoAgents  bool // nothing to select: zero writes (CodeUnsupportedAgent)
	Executed  bool
	AgentIDs  []string
	Result    app.InstallFromLockResult
	Err       error
}

// RunLockWizard runs the lock-install flow on the terminal. It refuses to
// start without a TTY (the CLI gates on interactivity before calling this).
func RunLockWizard(ctx context.Context, cfg LockWizardConfig, isTTY bool) (LockWizardOutcome, error) {
	if !isTTY {
		return LockWizardOutcome{}, fmt.Errorf("%w: the guided install requires an interactive terminal", errs.ErrUsage)
	}
	final, err := tea.NewProgram(newLockWizardModel(ctx, cfg)).Run()
	if err != nil {
		return LockWizardOutcome{}, fmt.Errorf("tui: %w", err)
	}
	m, ok := final.(lockWizardModel)
	if !ok {
		return LockWizardOutcome{}, fmt.Errorf("tui: unexpected final model %T", final)
	}
	return m.Outcome(), nil
}

// lockStep enumerates the flow's steps.
type lockStep int

const (
	lockStepAgents lockStep = iota
	lockStepPreview
	lockStepProgress
	lockStepSummary
	lockStepNoAgents
	lockStepError
)

// String labels steps for test failure messages.
func (s lockStep) String() string {
	switch s {
	case lockStepAgents:
		return "agents"
	case lockStepPreview:
		return "preview"
	case lockStepProgress:
		return "progress"
	case lockStepSummary:
		return "summary"
	case lockStepNoAgents:
		return "no-agents"
	case lockStepError:
		return "error"
	default:
		return fmt.Sprintf("step(%d)", int(s))
	}
}

type lockAgentsDoneMsg struct {
	choices []app.AgentChoice
	err     error
}

type lockExecDoneMsg struct {
	res app.InstallFromLockResult
	err error
}

// lockProgressMsg carries one install lifecycle event into the model
// (contracts/install-progress-events.md consumer adapter).
type lockProgressMsg struct {
	event app.InstallProgressEvent
}

// waitLockMsg delivers the next message from the execute stream.
func waitLockMsg(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// lockWizardModel is the bubbletea model for the flow.
type lockWizardModel struct {
	ctx context.Context //nolint:containedctx // bubbletea models carry the run context by design (same as wizardModel)
	cfg LockWizardConfig
	st  Theme

	width int
	step  lockStep

	agentsLoading bool
	choices       []app.AgentChoice
	pick          *[]int
	form          *huh.Form

	agentIDs  []string
	result    app.InstallFromLockResult
	execErr   error
	failed    error
	cancelled bool
	executed  bool

	prog    InstallProgress
	execCh  chan tea.Msg
	results InstallResults
	height  int

	// cancelRun aborts the in-flight install's context (spec 014 US4); the
	// program keeps draining events until the pipeline reports back, so the
	// partial result screen always renders before exit.
	cancelRun  context.CancelFunc
	cancelling bool
}

func newLockWizardModel(ctx context.Context, cfg LockWizardConfig) lockWizardModel {
	return lockWizardModel{
		ctx: ctx, cfg: cfg, st: DefaultTheme(), step: lockStepAgents,
		agentsLoading: true, prog: NewInstallProgress(),
	}
}

// Init kicks off agent detection.
func (m lockWizardModel) Init() tea.Cmd {
	phases := m.cfg.Phases
	ctx := m.ctx
	return func() tea.Msg {
		choices, err := phases.Agents(ctx)
		return lockAgentsDoneMsg{choices: choices, err: err}
	}
}

// Outcome reports the final result for the CLI.
func (m lockWizardModel) Outcome() LockWizardOutcome {
	return LockWizardOutcome{
		Cancelled: m.cancelled,
		NoAgents:  m.step == lockStepNoAgents,
		Executed:  m.executed,
		AgentIDs:  m.agentIDs,
		Result:    m.result,
		Err:       cmp.Or(m.execErr, m.failed),
	}
}

// Update implements tea.Model.
func (m lockWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.prog = m.prog.SetWidth(msg.Width)
		if m.executed {
			// The result component exists only after execution; sizing an
			// empty zero-value table on every pre-summary resize is waste.
			m.results = m.results.SetSize(msg.Width, msg.Height)
		}
		return m, nil
	case lockAgentsDoneMsg:
		return m.onAgentsLoaded(msg)
	case lockProgressMsg:
		m.prog = m.prog.Observe(msg.event)
		return m, waitLockMsg(m.execCh)
	case lockExecDoneMsg:
		m.result = msg.res
		m.execErr = msg.err
		m.executed = true
		m.results = NewInstallResults(msg.res.Skills).SetSize(m.width, m.height)
		m.step = lockStepSummary
		return m, nil
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	if m.step == lockStepAgents && m.form != nil {
		return m.formMsg(msg)
	}
	return m, nil
}

func (m lockWizardModel) onAgentsLoaded(msg lockAgentsDoneMsg) (tea.Model, tea.Cmd) {
	m.agentsLoading = false
	if msg.err != nil {
		if errors.Is(msg.err, errs.ErrUnsupportedAgent) {
			m.step = lockStepNoAgents
			return m, nil
		}
		m.failed = msg.err
		m.step = lockStepError
		return m, nil
	}
	if len(msg.choices) == 0 {
		m.step = lockStepNoAgents
		return m, nil
	}
	m.choices = msg.choices
	m.buildForm()
	return m, nil
}

// buildForm constructs the searchable agent multi-select with recorded or
// detected agents preselected (clarification Q1).
func (m *lockWizardModel) buildForm() {
	pick := new([]int)
	opts := make([]huh.Option[int], 0, len(m.choices))
	for i, c := range m.choices {
		label := Sanitize(c.DisplayName)
		if c.Detected {
			label += "  (detected)"
		}
		opts = append(opts, huh.NewOption(label, i).Selected(c.Preselected))
	}
	// No minimum-selection validation: confirming with zero agents selected is
	// a deliberate, allowed narrowing to "remove every managed target for
	// this project" (spec 013 FR-012/FR-017) — distinct from the separate
	// lockStepNoAgents state (reached when there are no choices to select
	// from at all, i.e. no agents were detected on this machine).
	ms := huh.NewMultiSelect[int]().
		Title("").
		Options(opts...).
		Filterable(true).
		Value(pick)
	m.pick = pick
	m.form = huh.NewForm(huh.NewGroup(ms)).
		WithTheme(m.st.Huh()).
		WithShowHelp(false).
		WithWidth(m.width)
	m.form.Init()
}

func (m lockWizardModel) onKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	switch m.step {
	case lockStepAgents:
		if k == "q" || k == keyCtrlC {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.form == nil {
			return m, nil
		}
		return m.formMsg(key)
	case lockStepPreview:
		switch k {
		case "enter", "y":
			m.step = lockStepProgress
			return m, m.startExec()
		case keyEsc, "b":
			m.step = lockStepAgents
			m.buildForm()
			return m, nil
		case "q", "n", keyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		}
	case lockStepProgress:
		return m.onProgressKey(k)
	case lockStepSummary:
		return m.onSummaryKey(key)
	case lockStepNoAgents, lockStepError:
		return m, tea.Quit
	}
	return m, nil
}

// onProgressKey handles keys during installation: esc/ctrl+c cancels the run
// context and never quits — the pipeline drains, the remaining entries land
// as cancelled/not-attempted, and the partial result screen shows before
// exit (FR-024/FR-025). Every other key is ignored.
func (m lockWizardModel) onProgressKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case keyEsc, keyCtrlC:
		if m.cancelRun != nil && !m.cancelling {
			m.cancelling = true
			m.cancelRun()
		}
	}
	return m, nil
}

// onSummaryKey routes result-screen keys: the table owns scrolling and the
// enter → detail → esc round trip; with no rows any key exits, preserving
// the "press any key" contract.
func (m lockWizardModel) onSummaryKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.results.HasRows() {
		return m, tea.Quit
	}
	next, exit := m.results.Update(key)
	m.results = next
	if exit {
		return m, tea.Quit
	}
	return m, nil
}

// formMsg drives the agent form and advances to the preview on completion.
func (m lockWizardModel) formMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.form.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		m.form = f
	}
	if m.form.State == huh.StateCompleted {
		ids := make([]string, 0, len(*m.pick))
		for _, i := range *m.pick {
			ids = append(ids, m.choices[i].ID)
		}
		m.agentIDs = ids
		m.step = lockStepPreview
		return m, nil
	}
	return m, cmd
}

// startExec launches the install, streaming lifecycle events over a buffered
// channel into the progress view (the onboarding wizard's startExecute
// pattern). The run context is cancellable so esc/ctrl+c can stop the
// pipeline between skills (spec 014 US4).
func (m *lockWizardModel) startExec() tea.Cmd {
	ch := make(chan tea.Msg, 64)
	m.execCh = ch
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	phases := m.cfg.Phases
	ids := m.agentIDs
	go func() {
		defer cancel()
		res, err := phases.Execute(ctx, ids, func(e app.InstallProgressEvent) {
			ch <- lockProgressMsg{event: e}
		})
		ch <- lockExecDoneMsg{res: res, err: err}
		close(ch)
	}()
	return waitLockMsg(ch)
}

// View implements tea.Model.
func (m lockWizardModel) View() string {
	switch m.step {
	case lockStepAgents:
		return m.viewAgents()
	case lockStepPreview:
		return m.viewPreview()
	case lockStepProgress:
		v := m.header("Installing skills") +
			m.st.Subtitle.Render("Fetching, verifying, and linking skills") + "\n\n" +
			m.prog.View()
		if m.cancelling {
			return v + "\n" + m.st.Warning.Render("cancelling — waiting for the current skill to stop safely…") + "\n" +
				m.hintLine("finishing up")
		}
		return v + m.hintLine("esc cancel")
	case lockStepSummary:
		return m.viewSummary()
	case lockStepNoAgents:
		return m.viewNoAgents()
	case lockStepError:
		return m.header("Something went wrong") + Sanitize(errText(m.failed)) + "\n\npress any key to exit\n"
	default:
		return ""
	}
}

func (m lockWizardModel) header(title string) string {
	return m.st.Title.Render(title) + "\n\n"
}

func (m lockWizardModel) hintLine(hints string) string {
	return "\n" + m.st.Hint.Render(hints) + "\n"
}

// viewAgents shows the lock summary (FR-013: lock path, skill count, names and
// sources) above the agent multi-select.
func (m lockWizardModel) viewAgents() string {
	var b strings.Builder
	b.WriteString(m.header("Install from " + Sanitize(m.cfg.LockPath)))
	fmt.Fprintf(&b, "Found %d skill(s) in %s:\n", len(m.cfg.Skills), Sanitize(m.cfg.LockPath))
	for _, s := range m.cfg.Skills {
		fmt.Fprintf(&b, "  • %s — %s\n", Sanitize(s.Name), Sanitize(s.Source))
	}
	b.WriteString("\nChoose target agents:\n")
	if m.agentsLoading || m.form == nil {
		b.WriteString("⏳ Detecting agents…\n")
		b.WriteString(m.hintLine("q cancel"))
		return b.String()
	}
	b.WriteString(m.form.View())
	b.WriteString(m.hintLine("↑/↓ move · space toggle · / filter · enter continue · q cancel"))
	return b.String()
}

// viewPreview shows the installation plan and asks for approval. Per skill it
// renders which agents are kept/added, which managed targets will be
// removed, and the resulting before/after lock value (spec 013 FR-006), so
// agent removal — including narrowing to zero agents (FR-012/FR-017) — is
// never a silent side effect of confirming.
func (m lockWizardModel) viewPreview() string {
	var b strings.Builder
	b.WriteString(m.header("Installation plan"))
	installFor := strings.Join(m.agentIDs, ", ")
	if installFor == "" {
		installFor = "(none)"
	}
	fmt.Fprintf(&b, "Install for: %s\n\n", Sanitize(installFor))
	for _, s := range m.cfg.Skills {
		fmt.Fprintf(&b, "  %s — %s\n", Sanitize(s.Name), Sanitize(s.Source))
		for _, line := range lockPreviewPlanLines(s.Agents, m.agentIDs) {
			fmt.Fprintf(&b, "    %s\n", Sanitize(line))
		}
	}
	b.WriteString(m.hintLine("enter/y approve · esc back · q cancel (nothing written yet)"))
	return b.String()
}

// lockPreviewPlanLines formats one skill's kept/added/removed agent plan for
// the preview screen, per contracts/cli-install-agent-replace.md's "TUI
// contract" and "Explicit empty selection" sections.
func lockPreviewPlanLines(prior, requested []string) []string {
	kept := app.IntersectStrings(prior, requested)
	added := app.Subtract(requested, prior)
	removed := app.Subtract(prior, requested)
	var lines []string
	// Kept and added are reported on separate lines, not merged, so the
	// screen actually distinguishes "already installed, left alone" from
	// "newly installed" per FR-006 — a merged line can't tell the user
	// which of the listed agents are getting a fresh install.
	if len(kept) > 0 {
		lines = append(lines, "keep: "+strings.Join(kept, ", "))
	}
	if len(added) > 0 {
		lines = append(lines, "add: "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		lines = append(lines, "Remove managed targets from: "+strings.Join(removed, ", "))
	}
	return lines
}

// viewSummary renders the detailed result screen (spec 014 US2): truthful
// counters, the failure table, and the drill-down detail view. The run error
// is shown once here — the CLI must not repeat a generic summary afterwards
// (FR-020).
func (m lockWizardModel) viewSummary() string {
	var b strings.Builder
	b.WriteString(m.results.View())
	if m.execErr != nil {
		// Shown once, inside the TUI; the CLI never repeats a generic summary
		// after the wizard exits (FR-020).
		b.WriteString("\n" + m.st.Error.Render(Sanitize(errText(m.execErr))) + "\n")
	}
	b.WriteString(m.hintLine(m.results.Hints()))
	return b.String()
}

// viewNoAgents implements clarification Q4: inform and exit, zero writes.
func (m lockWizardModel) viewNoAgents() string {
	var b strings.Builder
	b.WriteString(m.header("No supported agents detected"))
	b.WriteString("No supported agents were found on this machine.\n")
	b.WriteString("Nothing was installed or written.\n\n")
	b.WriteString("You can pass agents explicitly instead:\n")
	b.WriteString("  gskill install --agent <id>[,<id>...]\n")
	b.WriteString(m.hintLine("press any key to exit"))
	return b.String()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
