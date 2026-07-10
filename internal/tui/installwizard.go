package tui

import (
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
}

// LockWizardPhases are the app-layer use-cases the flow drives, injected by
// the CLI so the wizard stays view-pure and unit-testable.
type LockWizardPhases struct {
	Agents  func(context.Context) ([]app.AgentChoice, error)
	Execute func(context.Context, []string) (app.InstallFromLockResult, error)
	// Reconcile receives the user's conflict choice ("lock" or "manifest",
	// FR-023) before Execute runs. Only called when Conflicts is non-empty.
	Reconcile func(choice string)
}

// LockWizardConfig configures a lock-install wizard run.
type LockWizardConfig struct {
	LockPath string
	Skills   []LockWizardSkill
	// Conflicts are manifest/lock disagreement lines (FR-023); when non-empty
	// the flow opens with a which-side-wins step.
	Conflicts []string
	Phases    LockWizardPhases
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
	lockStepConflict
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
	case lockStepConflict:
		return "conflict"
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
}

func newLockWizardModel(ctx context.Context, cfg LockWizardConfig) lockWizardModel {
	step := lockStepAgents
	if len(cfg.Conflicts) > 0 {
		step = lockStepConflict
	}
	return lockWizardModel{ctx: ctx, cfg: cfg, st: DefaultTheme(), step: step, agentsLoading: true}
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
		Err:       firstError(m.execErr, m.failed),
	}
}

func firstError(errsIn ...error) error {
	for _, e := range errsIn {
		if e != nil {
			return e
		}
	}
	return nil
}

// Update implements tea.Model.
func (m lockWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case lockAgentsDoneMsg:
		return m.onAgentsLoaded(msg)
	case lockExecDoneMsg:
		m.result = msg.res
		m.execErr = msg.err
		m.executed = true
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
	ms := huh.NewMultiSelect[int]().
		Title("").
		Options(opts...).
		Filterable(true).
		Validate(func(v []int) error {
			if len(v) == 0 {
				return errors.New("select at least one agent (space toggles)")
			}
			return nil
		}).
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
	case lockStepConflict:
		return m.onConflictKey(k)
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
			return m, m.execCmd()
		case keyEsc, "b":
			m.step = lockStepAgents
			m.buildForm()
			return m, nil
		case "q", "n", keyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		}
	case lockStepProgress:
		return m, nil // installation is not interruptible from the view
	case lockStepSummary, lockStepNoAgents, lockStepError:
		return m, tea.Quit
	}
	return m, nil
}

// onConflictKey handles the which-side-wins step (FR-023).
func (m lockWizardModel) onConflictKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "l":
		return m.resolveConflict("lock")
	case "m":
		return m.resolveConflict("manifest")
	case "q", "n", keyEsc, keyCtrlC:
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// resolveConflict records the which-side-wins choice (FR-023) and moves on to
// agent selection.
func (m lockWizardModel) resolveConflict(choice string) (tea.Model, tea.Cmd) {
	if m.cfg.Phases.Reconcile != nil {
		m.cfg.Phases.Reconcile(choice)
	}
	m.step = lockStepAgents
	if m.form == nil && len(m.choices) > 0 {
		m.buildForm()
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

func (m lockWizardModel) execCmd() tea.Cmd {
	phases := m.cfg.Phases
	ctx := m.ctx
	ids := m.agentIDs
	return func() tea.Msg {
		res, err := phases.Execute(ctx, ids)
		return lockExecDoneMsg{res: res, err: err}
	}
}

// View implements tea.Model.
func (m lockWizardModel) View() string {
	switch m.step {
	case lockStepAgents:
		return m.viewAgents()
	case lockStepConflict:
		return m.viewConflict()
	case lockStepPreview:
		return m.viewPreview()
	case lockStepProgress:
		return m.header("Installing skills") + "⏳ Fetching, verifying, and linking…\n"
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

// viewPreview shows the installation plan and asks for approval.
func (m lockWizardModel) viewPreview() string {
	var b strings.Builder
	b.WriteString(m.header("Installation plan"))
	fmt.Fprintf(&b, "Install %d skill(s) for: %s\n\n", len(m.cfg.Skills), Sanitize(strings.Join(m.agentIDs, ", ")))
	for _, s := range m.cfg.Skills {
		fmt.Fprintf(&b, "  + %s — %s\n", Sanitize(s.Name), Sanitize(s.Source))
	}
	b.WriteString(m.hintLine("enter/y approve · esc back · q cancel (nothing written yet)"))
	return b.String()
}

// viewSummary reports per-skill outcomes.
func (m lockWizardModel) viewSummary() string {
	var b strings.Builder
	b.WriteString(m.header("Install summary"))
	for _, s := range m.result.Skills {
		mark := "✓"
		if s.Status == app.LockSkillFailed {
			mark = "✗"
		}
		fmt.Fprintf(&b, "  %s %s (%s)\n", mark, Sanitize(s.Name), Sanitize(s.Status))
	}
	if m.execErr != nil {
		b.WriteString("\n" + Sanitize(errText(m.execErr)) + "\n")
	}
	b.WriteString(m.hintLine("press any key to exit"))
	return b.String()
}

// viewConflict shows the manifest/lock differences and asks which side wins
// (FR-023). Nothing has been written when this step is on screen.
func (m lockWizardModel) viewConflict() string {
	var b strings.Builder
	b.WriteString(m.header("gskill.toml and skills-lock.json disagree"))
	for _, line := range m.cfg.Conflicts {
		b.WriteString("  ! " + Sanitize(line) + "\n")
	}
	b.WriteString("\nWhich side should win? (nothing has been changed yet)\n")
	b.WriteString("  l — use skills-lock.json (rewrite the manifest declarations)\n")
	b.WriteString("  m — use gskill.toml (rewrite the lock entries)\n")
	b.WriteString(m.hintLine("l lock wins · m manifest wins · q cancel"))
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
