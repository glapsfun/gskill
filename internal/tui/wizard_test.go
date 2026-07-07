package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
