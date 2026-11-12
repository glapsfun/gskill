package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
)

// dashDrive sends one message and then runs the commands it spawns to
// completion, delivering each produced message back to the model. It mirrors
// the Bubble Tea runtime closely enough to exercise the async plan/apply path
// synchronously (the fake service returns immediately).
func dashDrive(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	queue := []tea.Msg{msg}
	for i := 0; i < 50 && len(queue) > 0; i++ {
		cur := queue[0]
		queue = queue[1:]
		if cur == nil {
			continue
		}
		next, cmd := m.Update(cur)
		mm, ok := next.(model)
		if !ok {
			t.Fatalf("model type changed: %T", next)
		}
		m = mm
		if cmd != nil {
			if produced := cmd(); produced != nil {
				queue = append(queue, produced)
			}
		}
	}
	return m
}

// selectDeploy moves the cursor to the update-available "deploy" skill.
func selectDeploy(t *testing.T, m model) model {
	t.Helper()
	m = dashUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != "deploy" {
		t.Fatalf("selection = %q, want deploy", m.selected)
	}
	return m
}

// --- US2: update, version change, eligibility, unsupported target ---

func TestDashboard_UpdateFlowReviewConfirmApplyRefresh(t *testing.T) {
	t.Parallel()
	fs := &fakeService{
		skills:   dashSkills(),
		plan:     ProposedChange{FromRev: "0.9.2", ToRev: "0.10.0"},
		applyOut: []ActionOutcome{{Name: "deploy", Kind: ChangeUpdate, Result: "updated"}},
	}
	m := selectDeploy(t, newDash(t, fs, 80, 24))

	m = dashDrive(t, m, dashKey("u"))
	if m.mode != modeConfirm || m.proposed == nil {
		t.Fatalf("update intent did not open review: mode=%d proposed=%v", m.mode, m.proposed)
	}
	if v := m.View(); !strings.Contains(v, "0.9.2 → 0.10.0") {
		t.Errorf("review missing installed→target transition:\n%s", v)
	}

	m = dashDrive(t, m, dashKey("enter"))
	if fs.applyName != "deploy" {
		t.Errorf("apply ran for %q, want deploy", fs.applyName)
	}
	if m.mode != modeResults || !strings.Contains(m.View(), "updated") {
		t.Errorf("results not shown after apply:\n%s", m.View())
	}
	if m.selected != "deploy" {
		t.Errorf("selection not kept on deploy after refresh: %q", m.selected)
	}
	m = dashDrive(t, m, dashKey("x")) // any key dismisses results
	if m.mode != modeBrowse {
		t.Errorf("results not dismissed: mode=%d", m.mode)
	}
}

func TestDashboard_UpdateNotOfferedForIneligibleSkill(t *testing.T) {
	t.Parallel()
	// Selection starts on tui-design, which is up to date (not updatable).
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	m = dashDrive(t, m, dashKey("u"))
	if m.mode != modeBrowse {
		t.Errorf("update opened for an ineligible skill: mode=%d", m.mode)
	}
}

func TestDashboard_UpdateNotRefusedForBranchTrackedSkill(t *testing.T) {
	t.Parallel()
	// A branch-tracked skill: List/displayVersion shows the branch label while
	// the update plan's current revision is the commit sha. They differ by
	// formatter, not by change, so the stale guard must not refuse the update
	// (it compares the installed label captured at the start, not FromRev).
	skills := []DashboardSkill{{
		Name: "tracker", InstalledVersion: "main", SourceLabel: "acme/skills", Candidate: "a1b2c3d4e5f6",
		Repo:   source.Repo{Kind: source.RepoRemote, Key: "github.com/acme/skills", Label: "acme/skills"},
		Health: "outdated", UpdateStatus: "update-available", Policy: "main",
		Agents: []string{"claude"}, Markdown: "# tracker\n",
	}}
	fs := &fakeService{
		skills:   skills,
		plan:     ProposedChange{FromRev: "0f1e2d3c4b5a", ToRev: "a1b2c3d4e5f6"},
		applyOut: []ActionOutcome{{Name: "tracker", Kind: ChangeUpdate, Result: "updated"}},
	}
	m := newDash(t, fs, 80, 24) // tracker is the only skill, so it is selected
	m = dashDrive(t, m, dashKey("u"))
	if m.mode != modeConfirm {
		t.Fatalf("update intent did not open review: mode=%d", m.mode)
	}
	m = dashDrive(t, m, dashKey("enter"))
	if fs.applyName != "tracker" {
		t.Fatalf("branch-tracked update refused (applyName=%q): the stale guard must not compare FromRev to the branch label", fs.applyName)
	}
	if m.mode != modeResults || !strings.Contains(m.View(), "updated") {
		t.Errorf("update result not shown after apply:\n%s", m.View())
	}
}

func TestDashboard_VersionPickerThenConfirm(t *testing.T) {
	t.Parallel()
	fs := &fakeService{
		skills:   dashSkills(),
		versions: []string{"1.0.1", "1.0.0"},
		plan:     ProposedChange{FromRev: "1.0.1", ToRev: "1.0.0", DeclBefore: `tui-design = "^1.0.0"`, DeclAfter: `tui-design = "1.0.0"`},
		applyOut: []ActionOutcome{{Name: "tui-design", Kind: ChangeVersion, Result: "downgraded"}},
	}
	m := newDash(t, fs, 80, 24) // tui-design selected

	m = dashDrive(t, m, dashKey("v"))
	if m.mode != modeVersionPick || len(m.versions) != 2 {
		t.Fatalf("version picker did not load: mode=%d versions=%v", m.mode, m.versions)
	}
	// Move to the first listed version (index 1; index 0 is the "latest" row).
	m = dashUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = dashDrive(t, m, dashKey("enter"))
	if m.mode != modeConfirm || m.proposed == nil {
		t.Fatalf("picking a version did not open review: mode=%d", m.mode)
	}
	v := m.View()
	for _, want := range []string{"1.0.1 → 1.0.0", `- tui-design = "^1.0.0"`, `+ tui-design = "1.0.0"`} {
		if !strings.Contains(v, want) {
			t.Errorf("review missing %q:\n%s", want, v)
		}
	}
	m = dashDrive(t, m, dashKey("enter"))
	if fs.applyName != "tui-design" || m.mode != modeResults {
		t.Errorf("version change not applied: applyName=%q mode=%d", fs.applyName, m.mode)
	}
}

func TestDashboard_UnsupportedTargetReportedNothingWritten(t *testing.T) {
	t.Parallel()
	fs := &fakeService{skills: dashSkills(), versions: []string{"1.0.0"}, planErr: errFake}
	m := newDash(t, fs, 80, 24)

	m = dashDrive(t, m, dashKey("v"))
	m = dashUpdate(t, m, dashKey("9"), dashKey("9")) // type an unsupported target "99"
	m = dashDrive(t, m, dashKey("enter"))
	if m.mode != modeConfirm || m.planErr == "" {
		t.Fatalf("unsupported target not reported in place: mode=%d planErr=%q", m.mode, m.planErr)
	}
	if !strings.Contains(m.View(), "Cannot plan this action") {
		t.Errorf("review did not surface the planning failure:\n%s", m.View())
	}
	if fs.applyName != "" {
		t.Errorf("apply ran despite a failed plan: %q", fs.applyName)
	}
}

func TestDashboard_GroupUpdateReusesGroupedApply(t *testing.T) {
	t.Parallel()
	fs := &fakeService{
		skills: groupSkills(),
		applyOut: []ActionOutcome{
			{Name: "one", Kind: ChangeGroupUpdate, Result: "updated"},
			{Name: "two", Kind: ChangeGroupUpdate, Result: "failed", Detail: "boom"},
		},
	}
	m := newDash(t, fs, 80, 24)
	m = dashDrive(t, m, dashKey("g"))
	if m.mode != modeConfirm || m.proposed == nil || m.proposed.Kind != ChangeGroupUpdate {
		t.Fatalf("group update did not open review: mode=%d proposed=%v", m.mode, m.proposed)
	}
	m = dashDrive(t, m, dashKey("enter"))
	if fs.applyName != "github.com/acme/skills" {
		t.Errorf("group apply ran for %q, want the repository key", fs.applyName)
	}
	v := m.View()
	if !strings.Contains(v, "one") || !strings.Contains(v, "two") || !strings.Contains(v, "failed") {
		t.Errorf("partial group results not reported per identity:\n%s", v)
	}
}

// --- US4: identity-safety, revalidation, failure handling ---

func TestDashboard_ActionTargetsIdentityAfterRefresh(t *testing.T) {
	t.Parallel()
	fs := &fakeService{
		skills:   dashSkills(),
		plan:     ProposedChange{FromRev: "0.9.2", ToRev: "0.10.0"},
		applyOut: []ActionOutcome{{Name: "deploy", Kind: ChangeUpdate, Result: "updated"}},
	}
	m := selectDeploy(t, newDash(t, fs, 80, 24))
	m = dashDrive(t, m, dashKey("u")) // review open on deploy

	// A refresh arrives reordering the inventory before the user confirms.
	reordered := []DashboardSkill{dashSkills()[1], dashSkills()[0]}
	m = dashUpdate(t, m, inventoryMsg{skills: reordered})

	dashDrive(t, m, dashKey("enter"))
	if fs.applyName != "deploy" {
		t.Errorf("apply retargeted after refresh: ran for %q, want deploy", fs.applyName)
	}
}

func TestDashboard_StaleTransitionRefused(t *testing.T) {
	t.Parallel()
	fs := &fakeService{
		skills:   dashSkills(),
		plan:     ProposedChange{FromRev: "0.9.2", ToRev: "0.10.0"},
		applyOut: []ActionOutcome{{Name: "deploy", Kind: ChangeUpdate, Result: "updated"}},
	}
	m := selectDeploy(t, newDash(t, fs, 80, 24))
	m = dashDrive(t, m, dashKey("u"))

	// deploy is updated out from under the dashboard: now up to date.
	changed := dashSkills()
	changed[1].InstalledVersion = "0.10.0"
	changed[1].UpdateStatus = "up-to-date"
	changed[1].Candidate = ""
	m = dashUpdate(t, m, inventoryMsg{skills: changed})

	m = dashDrive(t, m, dashKey("enter"))
	if fs.applyName != "" {
		t.Errorf("a stale transition was applied: %q", fs.applyName)
	}
	if m.mode != modeResults || !strings.Contains(m.View(), "refused") {
		t.Errorf("stale transition not refused in place:\n%s", m.View())
	}
	if m.sessionErr == nil || !errsIsPartial(m.sessionErr) {
		t.Errorf("stale refusal session error = %v, want the partial-failure exit (FR-025)", m.sessionErr)
	}
}

func TestDashboard_FailedActionKeepsSessionAlive(t *testing.T) {
	t.Parallel()
	fs := &fakeService{
		skills:   dashSkills(),
		plan:     ProposedChange{FromRev: "0.9.2", ToRev: "0.10.0"},
		applyOut: []ActionOutcome{{Name: "deploy", Kind: ChangeUpdate, Result: "failed", Detail: "network down"}},
	}
	m := selectDeploy(t, newDash(t, fs, 80, 24))
	m = dashDrive(t, m, dashKey("u"))
	m = dashDrive(t, m, dashKey("enter"))

	if !strings.Contains(m.View(), "network down") {
		t.Errorf("failure detail not reported:\n%s", m.View())
	}
	if m.sessionErr == nil || !errsIsPartial(m.sessionErr) {
		t.Errorf("session error = %v, want a partial-install failure", m.sessionErr)
	}
	m = dashDrive(t, m, dashKey("x")) // dismiss and keep going
	if m.mode != modeBrowse {
		t.Errorf("session ended after a failure: mode=%d", m.mode)
	}
}

func errsIsPartial(err error) bool {
	return err != nil && strings.Contains(err.Error(), errs.ErrPartialInstall.Error())
}

// groupSkills returns two updatable skills sharing one repository, for the
// group-update flow.
func groupSkills() []DashboardSkill {
	repo := source.Repo{Kind: source.RepoRemote, Key: "github.com/acme/skills", Label: "acme/skills"}
	return []DashboardSkill{
		{
			Name: "one", InstalledVersion: "1.0.0", SourceLabel: "acme/skills", Candidate: "1.1.0",
			Repo: repo, Health: "outdated", UpdateStatus: "update-available", Policy: "^1.0.0",
			Agents: []string{"claude"}, Markdown: "# one\n",
		},
		{
			Name: "two", InstalledVersion: "2.0.0", SourceLabel: "acme/skills", Candidate: "2.1.0",
			Repo: repo, Health: "outdated", UpdateStatus: "update-available", Policy: "^2.0.0",
			Agents: []string{"claude"}, Markdown: "# two\n",
		},
	}
}
