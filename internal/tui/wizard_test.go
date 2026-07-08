package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
)

// Shell tests (spec 011 T009): step advance and auto-advance (FR-004), back
// navigation preserving the session (FR-007), and cancel-anywhere-pre-approval
// with zero side effects (FR-006). Steps' own behavior is tested separately.

// fakeSkills builds a small discovery catalog.
func fakeSkills(ids ...string) []discovery.DiscoveredSkill {
	out := make([]discovery.DiscoveredSkill, 0, len(ids))
	for _, id := range ids {
		out = append(out, discovery.DiscoveredSkill{ID: id, DisplayName: id, Valid: true})
	}
	return out
}

// fakePhases returns wizard phases backed by in-memory fakes, recording calls.
type phaseCalls struct {
	discover, plan, execute int
}

func fakePhases(calls *phaseCalls, skills []discovery.DiscoveredSkill, conflicts []app.PlanConflict) WizardPhases {
	return WizardPhases{
		Discover: func(context.Context) (app.DiscoverResult, error) {
			calls.discover++
			return app.DiscoverResult{Skills: skills}, nil
		},
		Plan: func(_ context.Context, s *Session) (app.InstallPlan, error) {
			calls.plan++
			plan := app.InstallPlan{Source: s.Source, Conflicts: conflicts}
			for _, sk := range s.Selected {
				plan.Actions = append(plan.Actions, app.PlannedAction{
					Skill: sk.ID, AgentID: "claude", Destination: "/tmp/dest/" + sk.ID,
				})
			}
			return plan, nil
		},
		Execute: func(_ context.Context, _ app.InstallPlan, progress func(app.ProgressEvent)) (app.AddResult, error) {
			calls.execute++
			if progress != nil {
				progress(app.ProgressEvent{Skill: "alpha", Stage: "install"})
			}
			return app.AddResult{Installed: []app.InstalledSkill{{Name: "alpha"}}}, nil
		},
	}
}

// drive pumps messages through the model, following returned commands until
// the queue drains (executing tea.Cmds synchronously, batch-aware).
func drive(t *testing.T, m wizardModel, msgs ...tea.Msg) wizardModel {
	t.Helper()

	queue := append([]tea.Msg(nil), msgs...)
	for len(queue) > 0 {
		msg := queue[0]
		queue = queue[1:]
		next, cmd := m.Update(msg)
		var ok bool
		m, ok = next.(wizardModel)
		if !ok {
			t.Fatalf("Update returned %T, want wizardModel", next)
		}
		queue = append(queue, runCmd(cmd)...)
	}
	return m
}

// runCmd executes a tea.Cmd tree synchronously and returns produced messages.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	if _, isQuit := msg.(tea.QuitMsg); isQuit {
		return nil
	}
	return []tea.Msg{msg}
}

// start initializes the model and drains Init's command.
func start(t *testing.T, m wizardModel) wizardModel {
	t.Helper()

	var msgs []tea.Msg
	msgs = append(msgs, runCmd(m.Init())...)
	msgs = append(msgs, win(100, 30))
	return drive(t, m, msgs...)
}

func TestWizard_HappyPathReachesSummary(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha", "beta"), nil),
	})

	m = start(t, m)
	if m.step != stepWelcome {
		t.Fatalf("step = %v, want welcome", m.step)
	}
	if calls.discover != 1 {
		t.Fatalf("discover calls = %d, want 1", calls.discover)
	}

	// welcome → select → (version, agents unanswered but shell may show them) →
	// preview → approve → progress → summary.
	m = drive(t, m, key("enter")) // leave welcome
	if m.step != stepSelect {
		t.Fatalf("step = %v, want select", m.step)
	}
	m = drive(t, m, key(" "), key("enter")) // toggle first skill, confirm
	m = advanceToPreview(t, m)
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview, view:\n%s", m.step, m.View())
	}
	if calls.plan == 0 {
		t.Fatal("plan never computed for preview")
	}
	m = drive(t, m, key("enter")) // approve
	if calls.execute != 1 {
		t.Fatalf("execute calls = %d, want 1", calls.execute)
	}
	if m.step != stepSummary {
		t.Fatalf("step = %v, want summary, view:\n%s", m.step, m.View())
	}
	if !strings.Contains(m.View(), "alpha") {
		t.Errorf("summary view missing installed skill:\n%s", m.View())
	}
}

// advanceToPreview presses enter through any intermediate question steps
// (version/agents) until the preview is reached, bounded to avoid loops.
func advanceToPreview(t *testing.T, m wizardModel) wizardModel {
	t.Helper()

	for i := 0; i < 4 && m.step != stepPreview; i++ {
		m = drive(t, m, key("enter"))
	}
	return m
}

func TestWizard_AutoAdvancesAnsweredSteps(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	skills := fakeSkills("alpha", "beta")
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{
			Source: "example/repo", SourceAnswered: true,
			Selected: skills[:1], SkillsAnswered: true,
			VersionAnswered: true,
			AgentIDs:        []string{"claude"}, AgentsAnswered: true,
		},
		Phases: fakePhases(&calls, skills, nil),
	})

	m = start(t, m)
	m = drive(t, m, key("enter")) // leave welcome
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview (select/version/agents all answered must be skipped)", m.step)
	}
	if !m.session.SkillsAnswered || len(m.session.Selected) != 1 {
		t.Error("pre-filled selection lost")
	}
}

func TestWizard_BackNavigationPreservesSession(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha", "beta"), nil),
	})

	m = start(t, m)
	m = drive(t, m, key("enter"))           // welcome → select
	m = drive(t, m, key(" "), key("enter")) // choose alpha → next
	m = advanceToPreview(t, m)
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview", m.step)
	}

	m = drive(t, m, key("esc")) // back from preview
	if m.step == stepPreview {
		t.Fatal("esc did not navigate back from preview")
	}
	if len(m.session.Selected) != 1 || m.session.Selected[0].ID != "alpha" {
		t.Errorf("selection lost on back-navigation: %+v", m.session.Selected)
	}

	// Forward again: the earlier answer is still there and the plan recomputes.
	plansBefore := calls.plan
	m = advanceToPreview(t, m)
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview after returning forward", m.step)
	}
	if calls.plan <= plansBefore {
		t.Error("plan not recomputed after back-navigation return")
	}
}

func TestWizard_CancelBeforeApprovalNeverExecutes(t *testing.T) {
	t.Parallel()

	for _, at := range []string{"welcome", "select", "preview"} {
		t.Run(at, func(t *testing.T) {
			t.Parallel()

			var calls phaseCalls
			m := newWizardModel(context.Background(), WizardConfig{
				Session: Session{Source: "example/repo", SourceAnswered: true},
				Phases:  fakePhases(&calls, fakeSkills("alpha", "beta"), nil),
			})
			m = start(t, m)
			switch at {
			case "select":
				m = drive(t, m, key("enter"))
			case "preview":
				m = drive(t, m, key("enter"), key(" "), key("enter"))
				m = advanceToPreview(t, m)
			}

			m = drive(t, m, key("q"))
			out := m.Outcome()
			if !out.Cancelled {
				t.Errorf("cancel at %s: Outcome.Cancelled = false", at)
			}
			if calls.execute != 0 {
				t.Errorf("cancel at %s: execute was called %d times, want 0", at, calls.execute)
			}
		})
	}
}

func TestWizard_ConflictBlocksApproval(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	conflicts := []app.PlanConflict{{Skill: "alpha", Kind: app.ConflictCrossSource, Detail: "name collision"}}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha"), conflicts),
	})
	m = start(t, m)
	m = drive(t, m, key("enter")) // welcome → select (single skill still shown per FR-001)
	m = drive(t, m, key(" "), key("enter"))
	m = advanceToPreview(t, m)
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview", m.step)
	}
	if !strings.Contains(m.View(), "collision") {
		t.Errorf("preview does not surface the conflict:\n%s", m.View())
	}

	m = drive(t, m, key("enter")) // approval must be blocked
	if calls.execute != 0 {
		t.Error("approve with conflicts executed the plan (FR-016)")
	}
	if m.step != stepPreview {
		t.Errorf("step = %v, want to remain on preview while conflicted", m.step)
	}
}

func TestWizard_ExecuteErrorIsTerminalAndReported(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("checksum mismatch")
	var calls phaseCalls
	phases := fakePhases(&calls, fakeSkills("alpha"), nil)
	phases.Execute = func(context.Context, app.InstallPlan, func(app.ProgressEvent)) (app.AddResult, error) {
		calls.execute++
		return app.AddResult{}, wantErr
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  phases,
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter"))
	m = advanceToPreview(t, m)
	m = drive(t, m, key("enter")) // approve → execute fails

	out := m.Outcome()
	if !errors.Is(out.Err, wantErr) {
		t.Errorf("Outcome.Err = %v, want %v", out.Err, wantErr)
	}
	if !strings.Contains(m.View(), "checksum mismatch") {
		t.Errorf("error not shown to the user:\n%s", m.View())
	}
}

// ---- US1 step-depth tests (spec 011 T012) -------------------------------------

func TestWizardView_WelcomeShowsDetection(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	skills := fakeSkills("alpha", "beta")
	skills = append(skills, discovery.DiscoveredSkill{ID: "broken", Valid: false})
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, skills, nil),
	})
	m = start(t, m)

	view := m.View()
	for _, want := range []string{"example/repo", "2", "(1 invalid)", "Nothing is written until you approve"} {
		if !strings.Contains(view, want) {
			t.Errorf("welcome view missing %q:\n%s", want, view)
		}
	}
}

func TestWizardSelect_SearchNarrowsList(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha", "beta", "gamma"), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter")) // → select

	m = drive(t, m, key("/"), key("b"), key("e"), key("t"))
	view := m.View()
	if !strings.Contains(view, "beta") {
		t.Errorf("filtered view lost the match:\n%s", view)
	}
	if strings.Contains(view, "alpha") || strings.Contains(view, "gamma") {
		t.Errorf("filter did not narrow the list:\n%s", view)
	}

	// The narrowed row is selectable and confirms.
	m = drive(t, m, key("esc"), key(" "), key("enter"))
	if len(m.session.Selected) != 1 || m.session.Selected[0].ID != "beta" {
		t.Errorf("selection after filtered toggle = %+v, want beta", m.session.Selected)
	}
}

func TestWizardSelect_RequiresAtLeastOne(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha", "beta"), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter")) // → select
	m = drive(t, m, key("enter")) // confirm with nothing chosen

	if m.step != stepSelect {
		t.Fatalf("step = %v, want to stay on select", m.step)
	}
	if !strings.Contains(m.View(), "at least one skill") {
		t.Errorf("missing validation message:\n%s", m.View())
	}
}

func TestWizardPreview_OffersExactlyThreeChoices(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha"), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter"))
	m = advanceToPreview(t, m)

	view := m.View()
	for _, want := range []string{"approve & install", "go back and edit", "cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("approval choices missing %q (FR-017):\n%s", want, view)
		}
	}
	if !strings.Contains(view, "/tmp/dest/alpha") {
		t.Errorf("preview missing destination path:\n%s", view)
	}
}

func TestWizardProgress_RendersEvents(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha", "beta"), nil),
	})
	m.step = stepProgress
	m.session.Selected = fakeSkills("alpha", "beta")
	m.events = []app.ProgressEvent{{Skill: "alpha", Stage: "install"}, {Skill: "alpha", Stage: "record"}}

	view := m.View()
	if !strings.Contains(view, "✓ alpha") && !strings.Contains(view, "✓") {
		t.Errorf("progress view does not mark completed skills:\n%s", view)
	}
	if !strings.Contains(view, "beta") {
		t.Errorf("progress view missing pending skill:\n%s", view)
	}
}

func TestWizardSummary_ShowsPathsAndNextCommands(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	phases := fakePhases(&calls, fakeSkills("alpha"), nil)
	phases.Execute = func(context.Context, app.InstallPlan, func(app.ProgressEvent)) (app.AddResult, error) {
		calls.execute++
		return app.AddResult{Installed: []app.InstalledSkill{{
			Name: "alpha", Targets: map[string]string{"claude": ".claude/skills/alpha"},
		}}}, nil
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  phases,
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter"))
	m = advanceToPreview(t, m)
	m = drive(t, m, key("enter")) // approve

	view := m.View()
	for _, want := range []string{".claude/skills/alpha", "gskill list", "gskill status", "gskill update", "gskill remove"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary missing %q (FR-021):\n%s", want, view)
		}
	}
}

func TestWizard_SanitizesHostileSkillStrings(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	hostile := []discovery.DiscoveredSkill{{
		ID: "evil\x1b[2Jskill", DisplayName: "evil\x1b]0;pwned\x07", RepoPath: "a\x1b[31mb", Valid: true,
	}}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, hostile, nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter")) // → select

	if v := m.View(); strings.Contains(v, "\x1b[2J") || strings.Contains(v, "\x1b]0;") || strings.Contains(v, "\x1b[31m") {
		t.Errorf("select view leaks raw escape sequences:\n%q", v)
	}
}

func TestWizard_ResolveSelectionPrefillsSkills(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	skills := fakeSkills("alpha", "beta")
	phases := fakePhases(&calls, skills, nil)
	phases.ResolveSelection = func(_ context.Context, disc app.DiscoverResult) ([]discovery.DiscoveredSkill, error) {
		return disc.Skills[:1], nil // what --skill alpha would resolve to
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  phases,
	})
	m = start(t, m)
	m = drive(t, m, key("enter")) // leave welcome

	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview (selection pre-resolved from flags must skip select)", m.step)
	}
	if len(m.session.Selected) != 1 || m.session.Selected[0].ID != "alpha" {
		t.Errorf("session.Selected = %+v, want alpha", m.session.Selected)
	}
}

// ---- US2: agent selection step (spec 011 T020) --------------------------------

func agentPhases(calls *phaseCalls, choices []app.AgentChoice) WizardPhases {
	phases := fakePhases(calls, fakeSkills("alpha"), nil)
	phases.Agents = func(context.Context) ([]app.AgentChoice, error) { return choices, nil }
	return phases
}

func TestWizardAgents_ListsMarksAndPreselects(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	choices := []app.AgentChoice{
		{ID: "claude", DisplayName: "Claude", Detected: true, Preselected: true},
		{ID: "codex", DisplayName: "Codex"},
		{ID: "cursor", DisplayName: "Cursor"},
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  agentPhases(&calls, choices),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter")) // welcome → select alpha → agents
	if m.step != stepAgents {
		t.Fatalf("step = %v, want agents", m.step)
	}

	view := m.View()
	for _, want := range []string{"Claude", "Codex", "Cursor", "(detected)"} {
		if !strings.Contains(view, want) {
			t.Errorf("agents view missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "[x] Claude") {
		t.Errorf("detected default agent not preselected (FR-014):\n%s", view)
	}

	// Continue with the preselection: session carries claude.
	m = drive(t, m, key("enter"))
	if len(m.session.AgentIDs) != 1 || m.session.AgentIDs[0] != "claude" {
		t.Errorf("session.AgentIDs = %v, want [claude]", m.session.AgentIDs)
	}
	if m.step != stepPreview {
		t.Errorf("step = %v, want preview after agents", m.step)
	}
}

func TestWizardAgents_RequiresAtLeastOne(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	choices := []app.AgentChoice{
		{ID: "claude", DisplayName: "Claude", Detected: true, Preselected: true},
		{ID: "codex", DisplayName: "Codex"},
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  agentPhases(&calls, choices),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter")) // → agents
	m = drive(t, m, key(" "))                             // deselect the preselected claude
	m = drive(t, m, key("enter"))                         // must be blocked

	if m.step != stepAgents {
		t.Fatalf("step = %v, want to stay on agents with zero selected (FR-014)", m.step)
	}
	if !strings.Contains(m.View(), "at least one agent") {
		t.Errorf("missing validation message:\n%s", m.View())
	}

	// Multi-select: pick both and continue.
	m = drive(t, m, key(" "), key("down"), key(" "), key("enter"))
	if len(m.session.AgentIDs) != 2 {
		t.Errorf("session.AgentIDs = %v, want two agents", m.session.AgentIDs)
	}
}

// ---- US3: version step (spec 011 T025) -----------------------------------------

func versionPhases(calls *phaseCalls, vl app.VersionList) WizardPhases {
	phases := fakePhases(calls, fakeSkills("alpha"), nil)
	phases.Versions = func(context.Context) (app.VersionList, error) { return vl, nil }
	return phases
}

func toVersionStep(t *testing.T, m wizardModel) wizardModel {
	t.Helper()

	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter")) // welcome → select alpha → version
	if m.step != stepVersion {
		t.Fatalf("step = %v, want version", m.step)
	}
	return m
}

func TestWizardVersion_PreselectsLatestAndPicksRelease(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	vl := app.VersionList{Candidates: []app.VersionCandidate{
		{Kind: app.VersionLatest, Label: "latest → v2.0.0"},
		{Kind: app.VersionRelease, Label: "v2.0.0", Ref: "v2.0.0", Metadata: "c2"},
		{Kind: app.VersionRelease, Label: "v1.0.0", Ref: "v1.0.0", Metadata: "c1"},
		{Kind: app.VersionBranch, Label: "main", Ref: "main"},
	}}
	m := toVersionStep(t, newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  versionPhases(&calls, vl),
	}))

	view := m.View()
	for _, want := range []string{"latest → v2.0.0", "v1.0.0", "main"} {
		if !strings.Contains(view, want) {
			t.Errorf("version view missing %q:\n%s", want, view)
		}
	}

	// Pick v1.0.0 (two rows down from the preselected latest).
	m = drive(t, m, key("down"), key("down"), key("enter"))
	if m.session.RefSpec != "v1.0.0" {
		t.Errorf("session.RefSpec = %q, want v1.0.0", m.session.RefSpec)
	}
	if m.session.VersionLabel != "v1.0.0" {
		t.Errorf("session.VersionLabel = %q, want v1.0.0", m.session.VersionLabel)
	}
}

func TestWizardVersion_DegradedShowsNoteAndAcceptsTypedRef(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	vl := app.VersionList{
		Candidates:     []app.VersionCandidate{{Kind: app.VersionLatest, Label: "latest"}},
		Degraded:       true,
		DegradedReason: "offline mode: version browsing needs the remote",
	}
	m := toVersionStep(t, newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  versionPhases(&calls, vl),
	}))

	if v := m.View(); !strings.Contains(v, "version browsing unavailable") || !strings.Contains(v, "offline mode") {
		t.Errorf("degraded note missing (FR-012):\n%s", v)
	}

	// Type an exact ref: move to the typed-ref row and enter it.
	m = drive(t, m, key("down"), key("enter")) // the synthetic "type an exact ref" row
	for _, r := range "v9.9.9" {
		m = drive(t, m, key(string(r)))
	}
	m = drive(t, m, key("enter"))
	if m.session.RefSpec != "v9.9.9" {
		t.Errorf("typed ref not applied: RefSpec = %q, want v9.9.9", m.session.RefSpec)
	}
}

func TestWizardVersion_TypedFullSHAPinsCommit(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("a1", 20) // 40 hex chars
	var calls phaseCalls
	vl := app.VersionList{Candidates: []app.VersionCandidate{{Kind: app.VersionLatest, Label: "latest"}}}
	m := toVersionStep(t, newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  versionPhases(&calls, vl),
	}))

	m = drive(t, m, key("down"), key("enter"))
	for _, r := range sha {
		m = drive(t, m, key(string(r)))
	}
	m = drive(t, m, key("enter"))
	if m.session.Commit != sha {
		t.Errorf("typed SHA not pinned as commit: Commit = %q", m.session.Commit)
	}
	if m.session.RefSpec != "" {
		t.Errorf("typed SHA must not set RefSpec, got %q", m.session.RefSpec)
	}
}

// ---- US4: deep preview and back-navigation (spec 011 T030) ---------------------

func TestWizardPreview_BoundedViewportScrolls(t *testing.T) {
	t.Parallel()

	ids := make([]string, 30)
	for i := range ids {
		ids[i] = fmt.Sprintf("skill-%02d", i)
	}
	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills(ids...), nil),
	})
	m = start(t, m)
	m = drive(t, m, win(80, 24))
	m = drive(t, m, key("enter")) // → select
	// Select all 30 via toggling each row.
	for range ids {
		m = drive(t, m, key(" "), key("down"))
	}
	m = drive(t, m, key("enter"))
	m = advanceToPreview(t, m)
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview", m.step)
	}

	view := m.View()
	if got := strings.Count(view, "\n"); got > 24 {
		t.Errorf("preview renders %d lines at a 24-row terminal; it must stay bounded", got)
	}
	if !strings.Contains(view, "more") {
		t.Errorf("bounded preview missing a more-content marker:\n%s", view)
	}

	before := m.View()
	m = drive(t, m, key("down"), key("down"), key("down"))
	if m.View() == before {
		t.Error("preview does not scroll on ↓")
	}
}

func TestWizardBackNavigation_EditSelectionUpdatesPlan(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha", "beta"), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"))                                  // → select
	m = drive(t, m, key(" "), key("down"), key(" "), key("enter")) // pick both
	m = advanceToPreview(t, m)
	if !strings.Contains(m.View(), "beta") {
		t.Fatalf("preview missing beta:\n%s", m.View())
	}

	// Go back (possibly through several steps) to the selection and drop beta.
	for i := 0; i < 4 && m.step != stepSelect; i++ {
		m = drive(t, m, key("esc"))
	}
	if m.step != stepSelect {
		t.Fatalf("could not navigate back to selection, stuck at %v", m.step)
	}
	m = drive(t, m, key("down"), key(" "), key("enter")) // deselect beta, confirm
	m = advanceToPreview(t, m)

	view := m.View()
	if strings.Contains(view, "+ beta") {
		t.Errorf("preview still plans beta after it was deselected:\n%s", view)
	}
	if !strings.Contains(view, "+ alpha") {
		t.Errorf("preview lost alpha:\n%s", view)
	}
}

// ---- US5: source-input step (spec 011 T033) -------------------------------------

func sourcelessConfig(calls *phaseCalls, validate func(string) error, suggestions []string) WizardConfig {
	phases := fakePhases(calls, fakeSkills("alpha"), nil)
	phases.ValidateSource = validate
	return WizardConfig{
		Session:           Session{}, // no source: wizard starts at the source step
		Phases:            phases,
		SourceSuggestions: suggestions,
	}
}

func TestWizardSource_ValidatesInlineAndRecovers(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	validate := func(s string) error {
		if s == "bad" {
			return errors.New("unrecognized source")
		}
		return nil
	}
	m := newWizardModel(context.Background(), sourcelessConfig(&calls, validate, nil))
	m = start(t, m)
	if m.step != stepSource {
		t.Fatalf("step = %v, want source", m.step)
	}

	m = drive(t, m, key("b"), key("a"), key("d"), key("enter"))
	if m.step != stepSource {
		t.Fatalf("invalid source advanced the flow (US5 scenario 2), step = %v", m.step)
	}
	if !strings.Contains(m.View(), "unrecognized source") {
		t.Errorf("validation error not shown inline:\n%s", m.View())
	}

	// Correct the input without leaving the flow.
	m = drive(t, m, key("backspace"), key("backspace"), key("backspace"))
	for _, r := range "example/repo" {
		m = drive(t, m, key(string(r)))
	}
	m = drive(t, m, key("enter"))
	if m.step != stepWelcome {
		t.Fatalf("corrected source did not continue to welcome, step = %v", m.step)
	}
	if m.session.Source != "example/repo" || calls.discover != 1 {
		t.Errorf("session.Source = %q, discover calls = %d", m.session.Source, calls.discover)
	}
}

func TestWizardSource_EmptyInputIsRejected(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), sourcelessConfig(&calls, nil, nil))
	m = start(t, m)
	m = drive(t, m, key("enter"))
	if m.step != stepSource {
		t.Fatalf("empty source advanced the flow, step = %v", m.step)
	}
	if !strings.Contains(m.View(), "enter a repository") {
		t.Errorf("missing empty-input guidance:\n%s", m.View())
	}
}

func TestWizardSource_ConfiguredSourcesSelectable(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), sourcelessConfig(&calls, nil,
		[]string{"github.com/acme/skills", "github.com/other/repo"}))
	m = start(t, m)

	if v := m.View(); !strings.Contains(v, "github.com/acme/skills") {
		t.Fatalf("configured sources not offered:\n%s", v)
	}
	// The first suggestion is under the cursor; enter selects it.
	m = drive(t, m, key("enter"))
	if m.session.Source != "github.com/acme/skills" {
		t.Errorf("session.Source = %q, want the picked suggestion", m.session.Source)
	}
	if m.step != stepWelcome {
		t.Errorf("step = %v, want welcome after picking a source", m.step)
	}
}

// ---- Polish: security sanitization table (spec 011 T039) -----------------------

// TestWizard_ViewsNeverLeakEscapeSequences injects hostile escape sequences
// into every remote-origin field each view renders (constitution VI).
func TestWizard_ViewsNeverLeakEscapeSequences(t *testing.T) {
	t.Parallel()

	const evil = "\x1b[2J\x1b]0;pwned\x07\x1b[31m"

	var calls phaseCalls
	base := func() wizardModel {
		m := newWizardModel(context.Background(), WizardConfig{
			Session: Session{Source: "safe" + evil, SourceAnswered: true},
			Phases:  fakePhases(&calls, fakeSkills("alpha"), nil),
		})
		return start(t, m)
	}

	views := map[string]func() string{
		"welcome with hostile source and warning": func() string {
			m := base()
			m.disc.Warnings = []string{"warn" + evil}
			return m.View()
		},
		"version with hostile label and metadata": func() string {
			m := base()
			m.step = stepVersion
			m.versions = app.VersionList{
				Candidates:     []app.VersionCandidate{{Kind: app.VersionRelease, Label: "v1" + evil, Metadata: "meta" + evil}},
				Degraded:       true,
				DegradedReason: "reason" + evil,
			}
			return m.View()
		},
		"preview with hostile conflict and action": func() string {
			m := base()
			m.step = stepPreview
			m.planReady = true
			m.plan = app.InstallPlan{
				Source:    "src" + evil,
				AgentIDs:  []string{"claude"},
				Actions:   []app.PlannedAction{{Skill: "s" + evil, AgentID: "claude", Destination: "d" + evil}},
				Conflicts: []app.PlanConflict{{Skill: "s", Kind: app.ConflictCrossSource, Detail: "boom" + evil}},
				Warnings:  []string{"w" + evil},
			}
			return m.View()
		},
		"summary with hostile names and targets": func() string {
			m := base()
			m.step = stepSummary
			m.result = app.AddResult{Installed: []app.InstalledSkill{{
				Name: "n" + evil, Targets: map[string]string{"claude": "t" + evil},
			}}}
			return m.View()
		},
		"error with hostile message": func() string {
			m := base()
			m.failed = errors.New("bad" + evil)
			return m.View()
		},
	}
	for name, render := range views {
		v := render()
		for _, seq := range []string{"\x1b[2J", "\x1b]0;", "\x1b[31m", "\x07"} {
			if strings.Contains(v, seq) {
				t.Errorf("%s leaks %q:\n%q", name, seq, v)
			}
		}
	}
}

// ---- Polish: SC-005 quantified responsiveness (spec 011 T040) -------------------

func TestWizardSelect_ResponsiveAt200PlusSkills(t *testing.T) {
	t.Parallel()

	ids := make([]string, 250)
	for i := range ids {
		ids[i] = fmt.Sprintf("skill-%03d-%s", i, strings.Repeat("x", i%7))
	}
	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills(ids...), nil),
	})
	m = start(t, m)
	m = drive(t, m, win(80, 24))

	startRender := time.Now()
	m = drive(t, m, key("enter")) // enter select: list built and rendered
	_ = m.View()
	if d := time.Since(startRender); d > 500*time.Millisecond {
		t.Errorf("selection list took %v to open at 250 skills, SC-005 requires < 500ms", d)
	}

	m = drive(t, m, key("/"))
	for _, r := range "skill-1" {
		startKey := time.Now()
		m = drive(t, m, key(string(r)))
		_ = m.View()
		if d := time.Since(startKey); d > 50*time.Millisecond {
			t.Errorf("filter keystroke %q took %v at 250 skills, SC-005 requires < 50ms", r, d)
		}
	}
}

// ---- Polish: end-to-end wizard over the real app phases (spec 011 T040) --------

// realProject builds an inited project with a .claude marker plus a two-skill
// local source, and wires wizard phases exactly as the CLI does.
func realPhases(t *testing.T, a *app.App, root, src string) WizardPhases {
	t.Helper()

	var disc app.DiscoverResult
	return WizardPhases{
		Discover: func(ctx context.Context) (app.DiscoverResult, error) {
			d, err := a.DiscoverSource(ctx, app.DiscoverRequest{Root: root, Source: src})
			if err != nil {
				return app.DiscoverResult{}, err
			}
			disc = d
			return d, nil
		},
		Plan: func(ctx context.Context, s *Session) (app.InstallPlan, error) {
			return a.PlanInstall(ctx, app.PlanRequest{
				Root: root, Source: src,
				Version: s.Version, Ref: s.RefSpec, Commit: s.Commit,
				Discover: disc, Selected: s.Selected, AgentIDs: s.AgentIDs,
			})
		},
		Execute: func(ctx context.Context, plan app.InstallPlan, progress func(app.ProgressEvent)) (app.AddResult, error) {
			return a.ExecutePlan(ctx, plan, progress)
		},
		Agents: func(ctx context.Context) ([]app.AgentChoice, error) {
			return a.AgentChoices(ctx, root)
		},
	}
}

func realWizardFixture(t *testing.T) (root, src string, a *app.App) {
	t.Helper()

	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	src = t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(src, "skills", name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a = app.New(app.Options{Agents: agent.NewDefaultRegistry(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := a.Init(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	return root, src, a
}

func TestWizard_EndToEndAgainstRealApp(t *testing.T) {
	t.Parallel()

	root, src, a := realWizardFixture(t)
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: src, SourceAnswered: true},
		Phases:  realPhases(t, a, root, src),
	})

	m = start(t, m)
	m = drive(t, m, key("enter"))           // welcome → select
	m = drive(t, m, key(" "), key("enter")) // pick alpha → agents
	m = drive(t, m, key("enter"))           // accept preselected claude → preview
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview", m.step)
	}
	m = drive(t, m, key("enter")) // approve → execute → summary
	if m.step != stepSummary {
		t.Fatalf("step = %v, want summary; failed=%v", m.step, m.failed)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("skill not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.lock")); err != nil {
		t.Errorf("lockfile not written: %v", err)
	}
}

func TestWizard_CancelAgainstRealAppWritesNothing(t *testing.T) {
	t.Parallel()

	root, src, a := realWizardFixture(t)
	manifestBefore, err := os.ReadFile(filepath.Join(root, "gskill.toml")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}

	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: src, SourceAnswered: true},
		Phases:  realPhases(t, a, root, src),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter"), key("enter")) // reach preview
	m = drive(t, m, key("q"))                                           // cancel at preview

	if !m.Outcome().Cancelled {
		t.Fatal("expected a cancelled outcome")
	}
	manifestAfter, err := os.ReadFile(filepath.Join(root, "gskill.toml")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if string(manifestBefore) != string(manifestAfter) {
		t.Error("cancel changed the manifest (SC-002)")
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.lock")); err == nil {
		t.Error("cancel left a lockfile behind (SC-002)")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha")); err == nil {
		t.Error("cancel left installed files behind (SC-002)")
	}
}

// ---- Phase 9 convergence tests (spec 011 T042–T046) ----------------------------

func describedSkills() []discovery.DiscoveredSkill {
	return []discovery.DiscoveredSkill{
		{ID: "alpha", DisplayName: "alpha", Description: "reviews pull requests", Valid: true},
		{ID: "beta", DisplayName: "beta", Description: "debugs kubernetes pods", Valid: true},
		{
			ID: "broken", DisplayName: "broken", Description: "half-written", Valid: false,
			Problems: []discovery.Diagnostic{{Severity: discovery.SeverityError, Message: "missing description in frontmatter"}},
		},
	}
}

func TestWizardSelect_FilterMatchesDescriptions(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, describedSkills(), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter")) // → select

	// "kubernetes" appears only in beta's description (FR-010).
	m = drive(t, m, key("/"))
	for _, r := range "kubernetes" {
		m = drive(t, m, key(string(r)))
	}
	view := m.View()
	if !strings.Contains(view, "beta") {
		t.Errorf("description filter lost the matching skill:\n%s", view)
	}
	if strings.Contains(view, "alpha") {
		t.Errorf("description filter did not narrow the list:\n%s", view)
	}
}

func TestWizardSelect_RowsShowDescriptions(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, describedSkills(), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), win(120, 30)) // wide enough for descriptions

	if v := m.View(); !strings.Contains(v, "reviews pull requests") {
		t.Errorf("selection rows missing skill descriptions (FR-009):\n%s", v)
	}
}

func TestWizardSelect_InvalidReasonShownUnderCursor(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, describedSkills(), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), win(120, 30))
	m = drive(t, m, key("down"), key("down")) // cursor onto "broken"

	if v := m.View(); !strings.Contains(v, "missing description in frontmatter") {
		t.Errorf("invalid-skill reason not available on the invalid row (FR-011):\n%s", v)
	}
}

func TestWizardWelcome_ReportsAgentsAndVersions(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	phases := fakePhases(&calls, fakeSkills("alpha", "beta"), nil)
	phases.Agents = func(context.Context) ([]app.AgentChoice, error) {
		return []app.AgentChoice{
			{ID: "claude", DisplayName: "Claude", Detected: true, Preselected: true},
			{ID: "codex", DisplayName: "Codex"},
		}, nil
	}
	phases.Versions = func(context.Context) (app.VersionList, error) {
		return app.VersionList{Candidates: []app.VersionCandidate{
			{Kind: app.VersionLatest, Label: "latest → v2.0.0"},
			{Kind: app.VersionRelease, Label: "v2.0.0", Ref: "v2.0.0"},
			{Kind: app.VersionRelease, Label: "v1.0.0", Ref: "v1.0.0"},
			{Kind: app.VersionBranch, Label: "main", Ref: "main"},
		}}, nil
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  phases,
	})
	m = start(t, m)

	view := m.View()
	if !strings.Contains(view, "1 detected") {
		t.Errorf("welcome does not report detected agents (FR-005/US1-AC1):\n%s", view)
	}
	if !strings.Contains(view, "2 release") {
		t.Errorf("welcome does not report available versions (FR-005/US1-AC1):\n%s", view)
	}
}

func TestWizardVersion_ListIsBoundedAndFollowsCursor(t *testing.T) {
	t.Parallel()

	candidates := []app.VersionCandidate{{Kind: app.VersionLatest, Label: "latest"}}
	for i := range 100 {
		candidates = append(candidates, app.VersionCandidate{
			Kind: app.VersionRelease, Label: fmt.Sprintf("v1.%d.0", i), Ref: fmt.Sprintf("v1.%d.0", i),
		})
	}
	var calls phaseCalls
	m := toVersionStep(t, newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  versionPhases(&calls, app.VersionList{Candidates: candidates}),
	}))
	m = drive(t, m, win(80, 24))

	if got := strings.Count(m.View(), "\n"); got > 24 {
		t.Errorf("version list renders %d lines at a 24-row terminal (FR-022)", got)
	}
	if !strings.Contains(m.View(), "more") {
		t.Errorf("bounded version list missing a more-content marker:\n%s", m.View())
	}

	// Cursor far down must remain visible.
	for range 60 {
		m = drive(t, m, key("down"))
	}
	if !strings.Contains(m.View(), "v1.59.0") {
		t.Errorf("cursor row not kept visible while scrolling:\n%s", m.View())
	}
	if got := strings.Count(m.View(), "\n"); got > 24 {
		t.Errorf("scrolled version list renders %d lines at a 24-row terminal", got)
	}
}

// ---- Review fixes, Phase B ------------------------------------------------------

func TestWizard_StalePlanDoneNeverAutoApprovesOffPreview(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true, ApprovalAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha", "beta"), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter")) // welcome → select; user is editing here

	// A plan message from an earlier, abandoned preview visit arrives late.
	stale := planDoneMsg{plan: app.InstallPlan{Source: "example/repo"}, gen: m.planGen}
	m = drive(t, m, stale)
	if calls.execute != 0 {
		t.Fatalf("stale planDoneMsg executed the plan while the user was on %v", m.step)
	}
	if m.step != stepSelect {
		t.Fatalf("step = %v, want to remain on select", m.step)
	}
}

func TestWizard_SupersededPlanResultIgnored(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  fakePhases(&calls, fakeSkills("alpha"), nil),
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter"))
	m = advanceToPreview(t, m)

	// A result from a superseded plan request (older generation) must not
	// overwrite the current plan state.
	m.planReady = false
	old := planDoneMsg{plan: app.InstallPlan{Source: "STALE"}, gen: m.planGen - 1}
	m = drive(t, m, old)
	if m.planReady {
		t.Fatal("superseded plan result marked the plan ready")
	}
	if m.plan.Source == "STALE" {
		t.Fatal("superseded plan result overwrote the current plan")
	}
}

// sourceAwarePhases returns phases whose discovery and version listing depend
// on the source chosen in the wizard, recording per-source calls.
func sourceAwarePhases(calls *phaseCalls, versionCalls *[]string) (WizardPhases, *string) {
	src := new(string)
	catalogs := map[string][]discovery.DiscoveredSkill{
		"srcA": fakeSkills("a1", "a2", "a3"),
		"srcB": fakeSkills("b1", "b2", "b3"),
	}
	phases := WizardPhases{
		SourceChosen: func(v string) { *src = v },
		Discover: func(context.Context) (app.DiscoverResult, error) {
			calls.discover++
			return app.DiscoverResult{Skills: catalogs[*src]}, nil
		},
		Versions: func(context.Context) (app.VersionList, error) {
			*versionCalls = append(*versionCalls, *src)
			return app.VersionList{Candidates: []app.VersionCandidate{
				{Kind: app.VersionLatest, Label: "latest of " + *src},
			}}, nil
		},
		Plan: func(_ context.Context, s *Session) (app.InstallPlan, error) {
			calls.plan++
			return app.InstallPlan{Source: s.Source}, nil
		},
		Execute: func(context.Context, app.InstallPlan, func(app.ProgressEvent)) (app.AddResult, error) {
			calls.execute++
			return app.AddResult{}, nil
		},
	}
	return phases, src
}

func typeString(t *testing.T, m wizardModel, s string) wizardModel {
	t.Helper()
	for _, r := range s {
		m = drive(t, m, key(string(r)))
	}
	return m
}

func TestWizard_SourceChangeResetsCatalogAndVersions(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	var versionCalls []string
	phases, _ := sourceAwarePhases(&calls, &versionCalls)
	m := newWizardModel(context.Background(), WizardConfig{Phases: phases})
	m = start(t, m)
	if m.step != stepSource {
		t.Fatalf("step = %v, want source", m.step)
	}

	m = typeString(t, m, "srcA")
	m = drive(t, m, key("enter")) // accept source; discovery lands
	m = drive(t, m, key("enter")) // welcome → select
	if v := m.View(); !strings.Contains(v, "a1") {
		t.Fatalf("select does not show source A's skills:\n%s", v)
	}

	// Back to the source step and switch to source B (same skill count).
	m = drive(t, m, key("esc"), key("esc"))
	if m.step != stepSource {
		t.Fatalf("step = %v, want source after backing out", m.step)
	}
	for range len("srcA") {
		m = drive(t, m, key("backspace"))
	}
	m = typeString(t, m, "srcB")
	m = drive(t, m, key("enter")) // accept source; discovery lands
	m = drive(t, m, key("enter")) // welcome → select

	view := m.View()
	if strings.Contains(view, "a1") || !strings.Contains(view, "b1") {
		t.Fatalf("select still shows source A's catalog after switching to B:\n%s", view)
	}
	m = drive(t, m, key(" "), key("enter")) // pick b1 → version step
	if len(m.session.Selected) != 1 || m.session.Selected[0].ID != "b1" {
		t.Fatalf("session.Selected = %+v, want b1 (what was shown)", m.session.Selected)
	}

	// The version listing must have been refetched for source B.
	if len(versionCalls) < 2 || versionCalls[len(versionCalls)-1] != "srcB" {
		t.Fatalf("version listing calls = %v, want a refetch for srcB", versionCalls)
	}
	if v := m.View(); !strings.Contains(v, "latest of srcB") {
		t.Fatalf("version step shows stale listing:\n%s", v)
	}
}

func TestWizard_ReconfirmDoesNotAliasEarlierPlanInput(t *testing.T) {
	t.Parallel()

	var seen [][]discovery.DiscoveredSkill
	var calls phaseCalls
	phases := fakePhases(&calls, fakeSkills("alpha", "beta"), nil)
	phases.Plan = func(_ context.Context, s *Session) (app.InstallPlan, error) {
		calls.plan++
		seen = append(seen, s.Selected) // keep the header, not a copy: aliasing detector
		return app.InstallPlan{Source: s.Source}, nil
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  phases,
	})
	m = start(t, m)
	m = drive(t, m, key("enter"), key(" "), key("enter")) // select alpha → preview
	m = advanceToPreview(t, m)

	// Back to selection, switch the choice to beta, re-confirm.
	for i := 0; i < 4 && m.step != stepSelect; i++ {
		m = drive(t, m, key("esc"))
	}
	m = drive(t, m, key(" "), key("down"), key(" "), key("enter")) // deselect alpha, select beta
	m = advanceToPreview(t, m)

	if len(seen) < 2 {
		t.Fatalf("plan called %d times, want ≥2", len(seen))
	}
	if len(seen[0]) != 1 || seen[0][0].ID != "alpha" {
		t.Fatalf("first plan input was mutated by the later confirmation: %+v", seen[0])
	}
	if len(seen[1]) != 1 || seen[1][0].ID != "beta" {
		t.Fatalf("second plan input = %+v, want beta", seen[1])
	}
}

func TestWizard_FlagAnsweredAgentsSurviveListingError(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	phases := fakePhases(&calls, fakeSkills("alpha"), nil)
	phases.Agents = func(context.Context) ([]app.AgentChoice, error) {
		return nil, errors.New("agent detection exploded")
	}
	m := newWizardModel(context.Background(), WizardConfig{
		Session: Session{
			Source: "example/repo", SourceAnswered: true,
			AgentIDs: []string{"claude"}, AgentsAnswered: true,
		},
		Phases: phases,
	})
	m = start(t, m)

	if m.failed != nil {
		t.Fatalf("wizard failed on an agents listing it never needed: %v", m.failed)
	}
	m = drive(t, m, key("enter"), key(" "), key("enter"))
	m = advanceToPreview(t, m)
	if m.step != stepPreview {
		t.Fatalf("step = %v, want preview (agents step is flag-answered)", m.step)
	}
}

func TestWizard_OnboardDiscoveryErrorReturnsToSource(t *testing.T) {
	t.Parallel()

	attempts := 0
	var calls phaseCalls
	phases, _ := sourceAwarePhases(&calls, new([]string))
	inner := phases.Discover
	phases.Discover = func(ctx context.Context) (app.DiscoverResult, error) {
		attempts++
		if attempts == 1 {
			return app.DiscoverResult{}, errors.New("repository not found")
		}
		return inner(ctx)
	}
	m := newWizardModel(context.Background(), WizardConfig{Phases: phases})
	m = start(t, m)

	m = typeString(t, m, "srcA")
	m = drive(t, m, key("enter")) // discover fails
	if m.failed != nil {
		t.Fatalf("discovery error was terminal; onboard must return to the source step: %v", m.failed)
	}
	if m.step != stepSource {
		t.Fatalf("step = %v, want source with inline error", m.step)
	}
	if !strings.Contains(m.View(), "repository not found") {
		t.Fatalf("inline error missing:\n%s", m.View())
	}

	// Correcting (same input, retry) proceeds.
	m = drive(t, m, key("enter"))
	if m.step != stepWelcome || m.failed != nil {
		t.Fatalf("retry did not proceed: step=%v failed=%v", m.step, m.failed)
	}
}

func TestWizardVersion_TypedModeResetsAfterCommit(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	vl := app.VersionList{Candidates: []app.VersionCandidate{{Kind: app.VersionLatest, Label: "latest"}}}
	m := toVersionStep(t, newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  versionPhases(&calls, vl),
	}))
	m = drive(t, m, key("down"), key("enter")) // typed-ref row
	m = typeString(t, m, "v1.2.3")
	m = drive(t, m, key("enter")) // commit → forward

	m = drive(t, m, key("esc")) // back to the version step
	if m.step != stepVersion {
		t.Fatalf("step = %v, want version", m.step)
	}
	if m.versionTyping {
		t.Fatal("typed-input mode still active on re-entry")
	}
	m = drive(t, m, key("up")) // must navigate, not append to a stale buffer
	if m.versionInput.value != "" {
		t.Fatalf("stale typed buffer: %q", m.versionInput.value)
	}
}

// ---- Review round 2, Phase 2: async generations ----------------------------------

func TestWizard_StaleDiscoveryResultDropped(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	var versionCalls []string
	phases, _ := sourceAwarePhases(&calls, &versionCalls)
	m := newWizardModel(context.Background(), WizardConfig{Phases: phases})
	m = start(t, m)

	m = typeString(t, m, "srcA")
	m = drive(t, m, key("enter")) // gen 1: A's catalog loads
	m = drive(t, m, key("esc"))   // back to source
	for range len("srcA") {
		m = drive(t, m, key("backspace"))
	}
	m = typeString(t, m, "srcB")
	m = drive(t, m, key("enter")) // gen 2: B's catalog loads

	// A slow gen-1 discovery result lands late: it must be dropped, leaving
	// the catalog and selector consistent with source B.
	stale := discoverDoneMsg{res: app.DiscoverResult{Skills: fakeSkills("a1")}, gen: m.sourceGen - 1}
	m = drive(t, m, stale)
	m = drive(t, m, key("enter")) // welcome → select
	if v := m.View(); !strings.Contains(v, "b1") || strings.Contains(v, "a1") {
		t.Fatalf("stale discovery overwrote source B's catalog:\n%s", v)
	}
	m = drive(t, m, key(" "), key("enter"))
	if len(m.session.Selected) != 1 || m.session.Selected[0].ID != "b1" {
		t.Fatalf("selection desynced from catalog: %+v", m.session.Selected)
	}
}

func TestWizardVersion_StaleListingDroppedAndCursorClamped(t *testing.T) {
	t.Parallel()

	candidates := []app.VersionCandidate{{Kind: app.VersionLatest, Label: "latest"}}
	for i := range 10 {
		candidates = append(candidates, app.VersionCandidate{Kind: app.VersionRelease, Label: fmt.Sprintf("v%d.0.0", i), Ref: fmt.Sprintf("v%d.0.0", i)})
	}
	var calls phaseCalls
	m := toVersionStep(t, newWizardModel(context.Background(), WizardConfig{
		Session: Session{Source: "example/repo", SourceAnswered: true},
		Phases:  versionPhases(&calls, app.VersionList{Candidates: candidates}),
	}))
	for range 8 {
		m = drive(t, m, key("down")) // cursor deep into the list
	}

	// A stale (older-generation) listing must be dropped outright.
	staleList := versionsDoneMsg{res: app.VersionList{Candidates: candidates[:1]}, gen: m.sourceGen - 1}
	m = drive(t, m, staleList)
	if len(m.versions.Candidates) != 11 {
		t.Fatalf("stale versions listing replaced the current one: %d candidates", len(m.versions.Candidates))
	}

	// A legitimate shrink (same generation) must clamp the cursor.
	fresh := versionsDoneMsg{res: app.VersionList{Candidates: candidates[:1]}, gen: m.sourceGen}
	m = drive(t, m, fresh)
	m = drive(t, m, key("enter")) // must not panic (index into 1-element slice)
	if m.failed != nil {
		t.Fatalf("unexpected failure after clamped listing: %v", m.failed)
	}
}

func TestWizard_SameSourceReacceptSkipsRediscovery(t *testing.T) {
	t.Parallel()

	var calls phaseCalls
	var versionCalls []string
	phases, _ := sourceAwarePhases(&calls, &versionCalls)
	m := newWizardModel(context.Background(), WizardConfig{Phases: phases})
	m = start(t, m)

	m = typeString(t, m, "srcA")
	m = drive(t, m, key("enter")) // discover once
	if calls.discover != 1 {
		t.Fatalf("discover calls = %d, want 1", calls.discover)
	}
	m = drive(t, m, key("esc"))   // welcome → source (input still "srcA")
	m = drive(t, m, key("enter")) // re-accept the same source

	if calls.discover != 1 {
		t.Fatalf("re-accepting the unchanged source refired discovery: %d calls", calls.discover)
	}
	if m.step != stepWelcome || m.discovering {
		t.Fatalf("step = %v discovering=%v, want welcome with the cached catalog", m.step, m.discovering)
	}
	m = drive(t, m, key("enter")) // welcome must not be blocked
	if m.step != stepSelect {
		t.Fatalf("step = %v, want select", m.step)
	}
}
