package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
)

// The guided onboarding wizard (spec 011). One Bubble Tea program owns a step
// state machine and an in-memory Session; all real work happens through the
// injected WizardPhases closures (the app-layer phase API), delivered as
// messages from tea.Cmds. Nothing is written before the approval step hands
// the plan to Execute (FR-017; SC-002 is structural).

// Session is the user's answers accumulated across steps (data-model.md). The
// *Answered flags mark answers supplied on the command line (FR-004): those
// steps are skipped, both forward and when navigating back.
type Session struct {
	Source         string
	SourceAnswered bool

	Selected       []discovery.DiscoveredSkill
	SkillsAnswered bool

	// Requested version pin (one of Version/RefSpec/Commit), plus a display
	// label for the preview.
	Version         string
	RefSpec         string
	Commit          string
	VersionLabel    string
	VersionAnswered bool

	AgentIDs       []string
	AgentsAnswered bool

	// ApprovalAnswered maps --yes: the approval step auto-approves a
	// conflict-free plan (it never bypasses a conflicted one, FR-016).
	ApprovalAnswered bool
}

// WizardPhases are the app-layer use-cases the wizard drives, injected by the
// CLI so the wizard stays view-pure and unit-testable (contracts/app-phases.md).
// Versions and Agents are optional; a nil closure skips that step.
type WizardPhases struct {
	Discover func(context.Context) (app.DiscoverResult, error)
	Versions func(context.Context) (app.VersionList, error)
	Agents   func(context.Context) ([]app.AgentChoice, error)
	Plan     func(context.Context, *Session) (app.InstallPlan, error)
	Execute  func(context.Context, app.InstallPlan, func(app.ProgressEvent)) (app.AddResult, error)
	// ValidateSource vets typed source input on the source step (US5).
	ValidateSource func(string) error
	// ResolveSelection, when set, resolves flag-given skill selectors against
	// the discovery result; the selection step is then skipped (FR-004).
	ResolveSelection func(context.Context, app.DiscoverResult) ([]discovery.DiscoveredSkill, error)
	// SourceChosen, when set, is called with the source accepted on the
	// source step, before discovery starts — the CLI's phase closures read
	// the chosen source through it (US5).
	SourceChosen func(string)
}

// WizardConfig configures a wizard run.
type WizardConfig struct {
	Session Session
	Phases  WizardPhases
	// SourceSuggestions are configured sources offered as a pick list on the
	// source step (US5), above the free-form input.
	SourceSuggestions []string
}

// WizardOutcome is what a finished wizard reports back to the CLI.
type WizardOutcome struct {
	Cancelled bool
	Executed  bool
	Result    app.AddResult
	Err       error
}

// RunWizard runs the guided flow on the terminal. It refuses to start without
// a TTY (FR-003; the CLI gates on interactivity before calling this).
func RunWizard(ctx context.Context, cfg WizardConfig, isTTY bool) (WizardOutcome, error) {
	if !isTTY {
		return WizardOutcome{}, fmt.Errorf("%w: the guided flow requires an interactive terminal", errs.ErrUsage)
	}
	final, err := tea.NewProgram(newWizardModel(ctx, cfg)).Run()
	if err != nil {
		return WizardOutcome{}, fmt.Errorf("tui: %w", err)
	}
	m, ok := final.(wizardModel)
	if !ok {
		return WizardOutcome{}, fmt.Errorf("tui: unexpected final model %T", final)
	}
	return m.Outcome(), nil
}

// stepID enumerates the wizard steps in canonical order.
type stepID int

const (
	stepSource stepID = iota
	stepWelcome
	stepSelect
	stepVersion
	stepAgents
	stepPreview
	stepProgress
	stepSummary
)

// String names steps for badges and diagnostics.
func (s stepID) String() string {
	switch s {
	case stepSource:
		return "source"
	case stepWelcome:
		return "welcome"
	case stepSelect:
		return "select skills"
	case stepVersion:
		return "version"
	case stepAgents:
		return "agents"
	case stepPreview:
		return "review & approve"
	case stepProgress:
		return "installing"
	case stepSummary:
		return "done"
	default:
		return "?"
	}
}

// Async phase messages. Source-scoped results (discovery, version listing)
// carry the sourceGen that requested them, so a slow result from an abandoned
// source can never land as current (review finding: stale-result races).
type discoverDoneMsg struct {
	res app.DiscoverResult
	err error
	gen int
}

type versionsDoneMsg struct {
	res app.VersionList
	err error
	gen int
}

type agentsDoneMsg struct {
	choices []app.AgentChoice
	err     error
}

type planDoneMsg struct {
	plan app.InstallPlan
	err  error
	// gen ties the result to the startPlan call that requested it, so a
	// superseded (backed-out, re-planned) request cannot land as current.
	gen int
}

type wizProgressMsg app.ProgressEvent

type executeDoneMsg struct {
	res app.AddResult
	err error
}

// wizardModel is the top-level program state: current step, session, async
// phase state, and the embedded per-step models.
type wizardModel struct {
	ctx     context.Context //nolint:containedctx // Bubble Tea commands are built from model state; the run context must travel with the model
	phases  WizardPhases
	st      wizardStyles
	session Session

	step    stepID
	history []stepID // visited steps, for back-navigation (FR-007)

	width, height int

	// Source-input step (US5).
	srcInput       lineInput
	srcErr         string
	srcSuggestions []string
	srcCursor      int // index into srcSuggestions; == len means the input row

	// Discovery (welcome step). sourceGen identifies the current source's
	// request wave; results from earlier waves are dropped.
	sourceGen   int
	discovering bool
	discovered  bool
	disc        app.DiscoverResult

	// Skill selection: the spec-009 selector embedded as a step (US1).
	sel       selectorModel
	selSource string // source the selector's items were built from
	selErr    string

	// Version step (US3).
	versions        app.VersionList
	versionsLoading bool
	versionCursor   int
	versionTyping   bool      // the "type an exact ref" row is active
	versionInput    lineInput // typed ref/commit buffer

	// Agents step (US2).
	agentChoices  []app.AgentChoice
	agentsLoading bool
	agentCursor   int
	agentChosen   map[int]bool
	agentErr      string

	// Preview / plan.
	planning      bool
	planReady     bool
	plan          app.InstallPlan
	planGen       int // generation of the most recent startPlan request
	previewOffset int // scroll offset into the preview body (bounded viewport)

	// sourceEditable marks a run that began without a source (the onboard
	// entry point): discovery failures return to the source step for inline
	// correction instead of ending the wizard (US5/AC2).
	sourceEditable bool

	// Execution.
	executing bool
	executed  bool
	events    []app.ProgressEvent
	execCh    chan tea.Msg
	result    app.AddResult

	failed    error // terminal failure (discover, plan, or execute)
	cancelled bool
}

func newWizardModel(ctx context.Context, cfg WizardConfig) wizardModel {
	m := wizardModel{
		ctx:         ctx,
		phases:      cfg.Phases,
		st:          defaultWizardStyles(),
		session:     cfg.Session,
		agentChosen: make(map[int]bool),
	}
	m.srcInput = newLineInput()
	for _, s := range cfg.SourceSuggestions {
		m.srcSuggestions = append(m.srcSuggestions, Sanitize(s))
	}
	if m.session.SourceAnswered {
		m.step = stepWelcome
		m.markWelcomeLoading()
	} else {
		m.step = stepSource
		m.sourceEditable = true
	}
	return m
}

// markWelcomeLoading flags the async loads Init (or the source step) is about
// to fire, so the welcome step renders loading states instead of empty data.
// Version and agent listings are fetched only when their step will be shown —
// a flag-answered step must never fetch (or fail on) data it will not use.
func (m *wizardModel) markWelcomeLoading() {
	m.sourceGen++
	m.discovering = true
	m.versionsLoading = m.phases.Versions != nil && !m.skipped(stepVersion) && len(m.versions.Candidates) == 0
	m.agentsLoading = m.phases.Agents != nil && !m.skipped(stepAgents) && len(m.agentChoices) == 0
}

// Init implements tea.Model: with the source known it kicks off discovery plus
// the agent and version listings, so the welcome step can report what was
// detected (FR-005). The corresponding loading flags were set by the
// constructor (Init cannot persist model mutations).
func (m wizardModel) Init() tea.Cmd {
	if m.step != stepWelcome {
		return nil
	}
	return m.welcomeLoads()
}

// welcomeLoads returns the batched commands for everything the welcome step
// reports on. Callers must have set the matching loading flags.
func (m wizardModel) welcomeLoads() tea.Cmd {
	cmds := []tea.Cmd{m.discoverCmd()}
	if m.versionsLoading {
		cmds = append(cmds, m.versionsCmd())
	}
	if m.agentsLoading {
		cmds = append(cmds, m.agentsCmd())
	}
	return tea.Batch(cmds...)
}

// Outcome reports the run's result to the CLI.
func (m wizardModel) Outcome() WizardOutcome {
	return WizardOutcome{Cancelled: m.cancelled, Executed: m.executed, Result: m.result, Err: m.failed}
}

// startDiscover launches phase 1 as a command.
func (m *wizardModel) startDiscover() tea.Cmd {
	m.discovering = true
	return m.discoverCmd()
}

// discoverCmd is the flag-free phase-1 command builder.
func (m wizardModel) discoverCmd() tea.Cmd {
	discover := m.phases.Discover
	ctx := m.ctx
	gen := m.sourceGen
	return func() tea.Msg {
		res, err := discover(ctx)
		return discoverDoneMsg{res: res, err: err, gen: gen}
	}
}

// startPlan launches phase 3 as a command; the preview renders when it lands.
// The session snapshot deep-copies Selected so a later re-confirmation cannot
// mutate the slice the in-flight plan is reading (review finding: data race).
func (m *wizardModel) startPlan() tea.Cmd {
	m.planning = true
	m.planReady = false
	m.planGen++
	gen := m.planGen
	plan := m.phases.Plan
	ctx := m.ctx
	session := m.session
	session.Selected = append([]discovery.DiscoveredSkill(nil), m.session.Selected...)
	return func() tea.Msg {
		p, err := plan(ctx, &session)
		return planDoneMsg{plan: p, err: err, gen: gen}
	}
}

// startExecute launches phase 4, streaming progress events over a channel.
func (m *wizardModel) startExecute() tea.Cmd {
	m.executing = true
	ch := make(chan tea.Msg, 64)
	m.execCh = ch
	execute := m.phases.Execute
	ctx := m.ctx
	plan := m.plan
	go func() {
		res, err := execute(ctx, plan, func(e app.ProgressEvent) {
			ch <- wizProgressMsg(e)
		})
		ch <- executeDoneMsg{res: res, err: err}
		close(ch)
	}()
	return waitWizardMsg(ch)
}

// waitWizardMsg delivers the next message from the execute stream.
func waitWizardMsg(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// skipped reports whether a question step is pre-answered or unavailable.
func (m *wizardModel) skipped(s stepID) bool {
	switch s {
	case stepSource:
		return m.session.SourceAnswered
	case stepSelect:
		return m.session.SkillsAnswered
	case stepVersion:
		return m.session.VersionAnswered || m.phases.Versions == nil
	case stepAgents:
		return m.session.AgentsAnswered || m.phases.Agents == nil
	case stepWelcome, stepPreview, stepProgress, stepSummary:
		return false
	default:
		return false
	}
}

// goForward advances to the next non-skipped step, recording history and
// firing the destination step's entry command.
func (m wizardModel) goForward() (wizardModel, tea.Cmd) {
	next := m.step + 1
	for next < stepPreview && m.skipped(next) {
		next++
	}
	m.history = append(m.history, m.step)
	return m.enterStep(next)
}

// goBack returns to the most recently visited step (skipping nothing: history
// only ever contains steps that were actually shown).
func (m wizardModel) goBack() (wizardModel, tea.Cmd) {
	if len(m.history) == 0 {
		return m, nil
	}
	prev := m.history[len(m.history)-1]
	m.history = m.history[:len(m.history)-1]
	m.step = prev
	m.planReady = false
	return m, nil
}

// enterStep switches to a step and runs its entry work.
func (m wizardModel) enterStep(s stepID) (wizardModel, tea.Cmd) {
	m.step = s
	switch s {
	case stepWelcome:
		if !m.discovered && !m.discovering {
			return m, m.startDiscover()
		}
	case stepSelect:
		m.syncSelector()
	case stepVersion, stepAgents:
		return m, m.loadChoices(s)
	case stepPreview:
		m.previewOffset = 0
		return m, m.startPlan()
	case stepProgress:
		return m, m.startExecute()
	case stepSource, stepSummary:
		// No entry work.
	}
	return m, nil
}

// loadChoices lazily fetches the version or agent candidates on step entry.
func (m *wizardModel) loadChoices(s stepID) tea.Cmd {
	if s == stepVersion && len(m.versions.Candidates) == 0 && !m.versionsLoading && m.phases.Versions != nil {
		return m.startVersions()
	}
	if s == stepAgents && len(m.agentChoices) == 0 && !m.agentsLoading && m.phases.Agents != nil {
		return m.startAgents()
	}
	return nil
}

// syncSelector (re)builds the selection step from the discovery catalog while
// preserving any earlier selection across back-navigation (FR-007). The guard
// is the source identity — never the item count — so switching sources always
// rebuilds even when the catalogs happen to be the same size (review finding).
func (m *wizardModel) syncSelector() {
	if m.selSource == m.session.Source && len(m.sel.items) > 0 {
		return // same catalog: keep cursor, filter, and chosen state
	}
	m.selSource = m.session.Source
	items := make([]SkillItem, len(m.disc.Skills))
	chosen := make(map[string]bool, len(m.session.Selected))
	for _, s := range m.session.Selected {
		chosen[s.ID] = true
	}
	m.sel = newSelectorModel(items)
	for i, s := range m.disc.Skills {
		// Every remote-origin string is sanitized before it can reach the
		// terminal (constitution VI: escape-sequence injection from SKILL.md).
		items[i] = SkillItem{
			ID:            Sanitize(s.ID),
			DisplayName:   Sanitize(s.DisplayName),
			Description:   Sanitize(s.Description),
			RepoPath:      Sanitize(s.RepoPath),
			Valid:         s.Valid,
			InvalidReason: Sanitize(s.FirstError()),
		}
		if chosen[s.ID] {
			m.sel.chosen[i] = true
		}
	}
	m.sel.items = items
	m.sel.recomputeVisible()
	m.sel.height, m.sel.width = m.height, m.width
}

// Update implements tea.Model.
func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.sel.height, m.sel.width = msg.Height, msg.Width
		m.sel.clamp()
		return m, nil

	case discoverDoneMsg:
		return m.onDiscoverDone(msg)

	case versionsDoneMsg:
		return m.onVersionsDone(msg)

	case agentsDoneMsg:
		return m.onAgentsDone(msg)

	case planDoneMsg:
		return m.onPlanDone(msg)

	case wizProgressMsg:
		m.events = append(m.events, app.ProgressEvent(msg))
		return m, waitWizardMsg(m.execCh)

	case executeDoneMsg:
		return m.onExecuteDone(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// onDiscoverDone lands phase 1's result on the welcome step. In a source-
// editable run (onboard), discovery failures — unreachable source, no skills —
// return to the source step with an inline error so the user can correct the
// input without the flow exiting (US5/AC2, review finding).
func (m wizardModel) onDiscoverDone(msg discoverDoneMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.sourceGen {
		return m, nil // result from an abandoned source: drop
	}
	m.discovering = false
	if msg.err != nil {
		return m.discoverFailed(msg.err)
	}
	if len(msg.res.Skills) == 0 {
		// Same category and message as the non-interactive path, so the exit
		// code matches (edge case: repository with no skills).
		return m.discoverFailed(fmt.Errorf("%w: no SKILL.md found in source", errs.ErrSourceUnavailable))
	}
	m.discovered = true
	m.disc = msg.res
	m.syncSelector()
	if m.phases.ResolveSelection != nil && !m.session.SkillsAnswered {
		selected, err := m.phases.ResolveSelection(m.ctx, m.disc)
		if err != nil {
			m.failed = err
			return m, nil
		}
		m.session.Selected = selected
		m.session.SkillsAnswered = true
	}
	return m, nil
}

// discoverFailed routes a discovery failure: back to the source step with an
// inline error when the source is user-editable, terminal otherwise (`add`).
func (m wizardModel) discoverFailed(err error) (tea.Model, tea.Cmd) {
	if !m.sourceEditable {
		m.failed = err
		return m, nil
	}
	m.srcErr = err.Error()
	m.step = stepSource
	if n := len(m.history); n > 0 && m.history[n-1] == stepSource {
		m.history = m.history[:n-1]
	}
	return m, nil
}

// onPlanDone lands phase 3's plan on the preview, auto-approving for --yes
// sessions when nothing conflicts. Results from superseded requests are
// dropped, and a result arriving after the user navigated away never
// auto-approves — re-entering the preview always recomputes (review finding:
// stale plan must not install against edited answers).
func (m wizardModel) onPlanDone(msg planDoneMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.planGen {
		return m, nil // superseded by a newer plan request
	}
	m.planning = false
	if m.step != stepPreview {
		return m, nil // user backed out; enterStep(stepPreview) will re-plan
	}
	if msg.err != nil {
		m.failed = msg.err
		return m, nil
	}
	m.plan = msg.plan
	m.planReady = true
	if m.session.ApprovalAnswered && len(m.plan.Conflicts) == 0 {
		return m.approve()
	}
	return m, nil
}

// onExecuteDone lands phase 4's outcome: summary on success, terminal error
// otherwise (rollback already happened inside the app layer, FR-020).
func (m wizardModel) onExecuteDone(msg executeDoneMsg) (tea.Model, tea.Cmd) {
	m.executing = false
	if msg.err != nil {
		m.failed = msg.err
		return m, tea.Quit
	}
	m.executed = true
	m.result = msg.res
	m.step = stepSummary
	return m, nil
}

// approve moves a conflict-free, ready plan into execution (FR-017).
func (m wizardModel) approve() (wizardModel, tea.Cmd) {
	if !m.planReady || len(m.plan.Conflicts) > 0 {
		return m, nil
	}
	m.history = append(m.history, m.step)
	return m.enterStep(stepProgress)
}

// handleKey routes keys: a terminal failure or the summary exits on any key;
// steps get first refusal (text inputs consume freely); the shell fallback is
// q/ctrl+c = cancel and esc/b = back (contracts/cli-onboarding.md).
func (m wizardModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.failed != nil {
		return m, tea.Quit
	}
	if m.executing {
		return m, nil // installs are not interruptible from the key loop
	}
	if m.step == stepSummary {
		switch key.String() {
		case keyEnter, "q", keyEsc, keyCtrlC:
			return m, tea.Quit
		}
		return m, nil
	}

	next, cmd, handled := m.stepKey(key)
	if handled {
		return next, cmd
	}
	switch key.String() {
	case keyCtrlC, "q":
		next.cancelled = true
		return next, tea.Quit
	case keyEsc, "b":
		return next.goBack()
	}
	return next, nil
}

// View implements tea.Model.
func (m wizardModel) View() string {
	if m.failed != nil {
		return m.viewError()
	}
	switch m.step {
	case stepSource:
		return m.viewSource()
	case stepWelcome:
		return m.viewWelcome()
	case stepSelect:
		return m.viewSelect()
	case stepVersion:
		return m.viewVersion()
	case stepAgents:
		return m.viewAgents()
	case stepPreview:
		return m.viewPreview()
	case stepProgress:
		return m.viewProgress()
	case stepSummary:
		return m.viewSummary()
	default:
		return ""
	}
}
