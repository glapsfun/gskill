package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/app"
)

// lockWizardFixture builds a model plus a recorder for the Execute phase.
type lockWizardFixture struct {
	m        lockWizardModel
	executed *[][]string
}

func newLockFixture(t *testing.T, choices []app.AgentChoice) lockWizardFixture {
	t.Helper()
	executed := &[][]string{}
	cfg := LockWizardConfig{
		LockPath: "skills-lock.json",
		Skills: []LockWizardSkill{
			{Name: "deploy-to-vercel", Source: "vercel-labs/agent-skills"},
			{Name: "web-design", Source: "vercel-labs/agent-skills"},
		},
		Phases: LockWizardPhases{
			Agents: func(context.Context) ([]app.AgentChoice, error) { return choices, nil },
			Execute: func(_ context.Context, ids []string) (app.InstallFromLockResult, error) {
				*executed = append(*executed, ids)
				return app.InstallFromLockResult{
					Agents: ids,
					Skills: []app.LockSkillResult{
						{Name: "deploy-to-vercel", Status: app.LockSkillInstalled},
						{Name: "web-design", Status: app.LockSkillInstalled},
					},
					Changed: true,
				}, nil
			},
		},
	}
	return lockWizardFixture{m: newLockWizardModel(context.Background(), cfg), executed: executed}
}

func twoAgents() []app.AgentChoice {
	return []app.AgentChoice{
		{ID: "claude", DisplayName: "Claude Code", Detected: true, Preselected: true},
		{ID: "codex", DisplayName: "Codex CLI", Detected: true},
	}
}

// lockDrive pumps messages through the model, following returned commands
// until the queue drains (same contract as the onboarding wizard's drive).
func lockDrive(t *testing.T, m lockWizardModel, msgs ...tea.Msg) lockWizardModel {
	t.Helper()
	queue := append([]tea.Msg(nil), msgs...)
	for len(queue) > 0 {
		msg := queue[0]
		queue = queue[1:]
		next, cmd := m.Update(msg)
		var ok bool
		m, ok = next.(lockWizardModel)
		if !ok {
			t.Fatalf("Update returned %T, want lockWizardModel", next)
		}
		queue = append(queue, runCmd(cmd)...)
	}
	return m
}

func lockKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func loadedLockModel(t *testing.T, f lockWizardFixture) lockWizardModel {
	t.Helper()
	choices, err := f.m.cfg.Phases.Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return lockDrive(t, f.m, lockAgentsDoneMsg{choices: choices})
}

// TestLockWizard_WelcomeShowsLockAndSkills (FR-013): the flow presents the
// detected lock file, the number of skills, and each skill's name and source.
func TestLockWizard_WelcomeShowsLockAndSkills(t *testing.T) {
	t.Parallel()
	m := loadedLockModel(t, newLockFixture(t, twoAgents()))
	view := m.View()
	for _, want := range []string{
		"skills-lock.json",
		"2 skill",
		"deploy-to-vercel",
		"web-design",
		"vercel-labs/agent-skills",
		"Claude Code",
		"Codex CLI",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("agent step view missing %q:\n%s", want, view)
		}
	}
}

// TestLockWizard_PreselectionAndConfirm: recorded agents come preselected;
// confirming the plan runs Execute with the picked IDs and shows the summary.
func TestLockWizard_PreselectionAndConfirm(t *testing.T) {
	t.Parallel()
	f := newLockFixture(t, twoAgents())
	m := loadedLockModel(t, f)

	// Accept the preselected agents.
	m = lockDrive(t, m, lockKey("enter"))
	if m.step != lockStepPreview {
		t.Fatalf("step = %v after agent submit, want preview", m.step)
	}
	view := m.View()
	for _, want := range []string{"claude", "deploy-to-vercel", "web-design"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview missing %q:\n%s", want, view)
		}
	}

	// Approve: the pump runs Execute synchronously and lands on the summary.
	m = lockDrive(t, m, lockKey("enter"))
	if m.step != lockStepSummary {
		t.Fatalf("step = %v after approval, want summary", m.step)
	}
	out := m.Outcome()
	if !out.Executed || out.Cancelled {
		t.Errorf("outcome = %+v, want executed", out)
	}
	if len(out.AgentIDs) != 1 || out.AgentIDs[0] != "claude" {
		t.Errorf("AgentIDs = %v, want preselected claude", out.AgentIDs)
	}
	if len(*f.executed) != 1 || (*f.executed)[0][0] != "claude" {
		t.Errorf("Execute calls = %v, want one with claude", *f.executed)
	}
}

// TestLockWizard_MultiSelect: toggling a second agent includes it.
func TestLockWizard_MultiSelect(t *testing.T) {
	t.Parallel()
	f := newLockFixture(t, twoAgents())
	m := loadedLockModel(t, f)

	// Move to the second option and toggle it, then submit.
	m = lockDrive(t, m, lockKey("down"))
	m = lockDrive(t, m, lockKey(" "))
	m = lockDrive(t, m, lockKey("enter"))
	if m.step != lockStepPreview {
		t.Fatalf("step = %v, want preview", m.step)
	}
	if len(m.agentIDs) != 2 {
		t.Fatalf("agentIDs = %v, want both", m.agentIDs)
	}
}

// TestLockWizard_CancelBeforeApproval: quitting anywhere pre-approval reports
// Cancelled and never calls Execute (zero writes, CodeCancelled at the CLI).
func TestLockWizard_CancelBeforeApproval(t *testing.T) {
	t.Parallel()
	f := newLockFixture(t, twoAgents())
	m := loadedLockModel(t, f)

	m = lockDrive(t, m, lockKey("enter")) // to preview
	m = lockDrive(t, m, lockKey("q"))     // cancel
	out := m.Outcome()
	if !out.Cancelled || out.Executed {
		t.Errorf("outcome = %+v, want cancelled, not executed", out)
	}
	if len(*f.executed) != 0 {
		t.Errorf("Execute was called %d times on a cancelled run", len(*f.executed))
	}
}

// TestLockWizard_EscGoesBack: esc on the preview returns to agent selection.
func TestLockWizard_EscGoesBack(t *testing.T) {
	t.Parallel()
	m := loadedLockModel(t, newLockFixture(t, twoAgents()))
	m = lockDrive(t, m, lockKey("enter"))
	m = lockDrive(t, m, lockKey("esc"))
	if m.step != lockStepAgents {
		t.Fatalf("step = %v after esc, want agents", m.step)
	}
	if m.Outcome().Cancelled {
		t.Error("esc back must not cancel the run")
	}
}

// TestLockWizard_NoAgentsDetected (clarification Q4): inform and exit with
// zero writes; the outcome maps to CodeUnsupportedAgent at the CLI.
func TestLockWizard_NoAgentsDetected(t *testing.T) {
	t.Parallel()
	f := newLockFixture(t, nil)
	m := lockDrive(t, f.m, lockAgentsDoneMsg{choices: nil})
	if m.step != lockStepNoAgents {
		t.Fatalf("step = %v, want noAgents", m.step)
	}
	view := m.View()
	for _, want := range []string{"No supported agents", "--agent"} {
		if !strings.Contains(view, want) {
			t.Errorf("no-agents view missing %q:\n%s", want, view)
		}
	}
	out := m.Outcome()
	if !out.NoAgents || out.Executed {
		t.Errorf("outcome = %+v, want NoAgents", out)
	}
	if len(*f.executed) != 0 {
		t.Error("Execute called despite no agents")
	}
}

// TestLockWizard_ExecFailureShowsError: a failed execution surfaces the error
// and reports it in the outcome.
func TestLockWizard_ExecFailureShowsError(t *testing.T) {
	t.Parallel()
	m := loadedLockModel(t, newLockFixture(t, twoAgents()))
	m = lockDrive(t, m, lockKey("enter")) // to preview
	m = lockDrive(t, m, lockExecDoneMsg{res: app.InstallFromLockResult{
		Skills: []app.LockSkillResult{{Name: "deploy-to-vercel", Status: app.LockSkillFailed}},
	}, err: context.DeadlineExceeded})
	if m.step != lockStepSummary {
		t.Fatalf("step = %v, want summary (with error)", m.step)
	}
	view := m.View()
	if !strings.Contains(view, "deadline") {
		t.Errorf("summary should surface the error:\n%s", view)
	}
	out := m.Outcome()
	if !out.Executed || out.Err == nil {
		t.Errorf("outcome = %+v, want executed with error", out)
	}
}

// TestLockWizard_ConflictStepAsksWhichSideWins (FR-023): a manifest/lock
// disagreement is shown first and the user picks the winning side.
func TestLockWizard_ConflictStepAsksWhichSideWins(t *testing.T) {
	t.Parallel()
	f := newLockFixture(t, twoAgents())
	var chosen string
	f.m.cfg.Conflicts = []string{`skill "alpha": source: gskill.toml says "x", skills-lock.json says "y"`}
	f.m.cfg.Phases.Reconcile = func(c string) { chosen = c }
	m := newLockWizardModel(context.Background(), f.m.cfg)
	if m.step != lockStepConflict {
		t.Fatalf("step = %v, want conflict first", m.step)
	}
	view := m.View()
	for _, want := range []string{"disagree", "alpha", "gskill.toml", "skills-lock.json"} {
		if !strings.Contains(view, want) {
			t.Errorf("conflict view missing %q:\n%s", want, view)
		}
	}

	m = lockDrive(t, m, lockKey("l"))
	if chosen != "lock" {
		t.Errorf("Reconcile choice = %q, want lock", chosen)
	}
	if m.step != lockStepAgents {
		t.Errorf("step = %v after choice, want agents", m.step)
	}
}

// TestLockWizard_ConflictManifestChoice: "m" picks the manifest side.
func TestLockWizard_ConflictManifestChoice(t *testing.T) {
	t.Parallel()
	f := newLockFixture(t, twoAgents())
	var chosen string
	f.m.cfg.Conflicts = []string{"skill x: source differs"}
	f.m.cfg.Phases.Reconcile = func(c string) { chosen = c }
	m := newLockWizardModel(context.Background(), f.m.cfg)
	m = lockDrive(t, m, lockKey("m"))
	if chosen != "manifest" {
		t.Errorf("Reconcile choice = %q, want manifest", chosen)
	}
	if m.step != lockStepAgents {
		t.Errorf("step = %v, want agents", m.step)
	}
}

// TestLockWizard_ConflictCancel: quitting at the conflict step cancels with
// zero writes.
func TestLockWizard_ConflictCancel(t *testing.T) {
	t.Parallel()
	f := newLockFixture(t, twoAgents())
	f.m.cfg.Conflicts = []string{"skill x: source differs"}
	m := newLockWizardModel(context.Background(), f.m.cfg)
	m = lockDrive(t, m, lockKey("q"))
	out := m.Outcome()
	if !out.Cancelled || out.Executed {
		t.Errorf("outcome = %+v, want cancelled", out)
	}
	if len(*f.executed) != 0 {
		t.Error("Execute called after cancel at conflict step")
	}
}
