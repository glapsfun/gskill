package tui

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// term builds a terminal event for skill name at index k of total.
func term(k, total int, name string, status app.InstallStatus) app.InstallProgressEvent {
	return app.InstallProgressEvent{
		SkillIndex: k, SkillTotal: total, SkillName: name,
		Phase: app.InstallPhaseComplete, Status: status,
	}
}

// running builds a running event in the given phase.
func running(k, total int, name string, phase app.InstallPhase) app.InstallProgressEvent {
	return app.InstallProgressEvent{
		SkillIndex: k, SkillTotal: total, SkillName: name,
		Phase: phase, Status: app.InstallStatusRunning,
	}
}

func TestInstallProgress_FractionMonotonic(t *testing.T) {
	t.Parallel()

	m := NewInstallProgress()
	m = m.SetWidth(100)
	if got := m.Percent(); got != 0 {
		t.Errorf("initial Percent() = %d, want 0", got)
	}

	m = m.Observe(running(1, 3, "alpha", app.InstallPhaseResolving))
	if got := m.Percent(); got != 0 {
		t.Errorf("Percent() after running event = %d, want 0 (progress is terminal-driven)", got)
	}
	if !strings.Contains(m.View(), "0 / 3") {
		t.Errorf("view lacks 0 / 3 counter:\n%s", m.View())
	}

	last := 0
	for k, name := range []string{"alpha", "beta", "gamma"} {
		m = m.Observe(running(k+1, 3, name, app.InstallPhaseStoring))
		m = m.Observe(term(k+1, 3, name, app.InstallStatusInstalled))
		if got := m.Percent(); got < last {
			t.Errorf("Percent() decreased %d -> %d", last, got)
		} else {
			last = got
		}
	}
	if got := m.Percent(); got != 100 {
		t.Errorf("Percent() after all terminals = %d, want 100", got)
	}
	if !m.Done() {
		t.Error("Done() = false after all terminal events")
	}
	if !strings.Contains(m.View(), "3 / 3") || !strings.Contains(m.View(), "100%") {
		t.Errorf("final view lacks 3 / 3 and 100%%:\n%s", m.View())
	}
}

func TestInstallProgress_CountersAndFailures(t *testing.T) {
	t.Parallel()

	m := NewInstallProgress().SetWidth(100)
	m = m.Observe(term(1, 4, "a", app.InstallStatusInstalled))
	m = m.Observe(term(2, 4, "b", app.InstallStatusFailed))
	m = m.Observe(term(3, 4, "c", app.InstallStatusUpToDate))

	view := m.View()
	for _, want := range []string{"Completed: 2", "Failed: 1", "Remaining: 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if m.Done() {
		t.Error("Done() = true with one skill remaining")
	}
}

func TestInstallProgress_CurrentSkillPhaseAndSanitize(t *testing.T) {
	t.Parallel()

	m := NewInstallProgress().SetWidth(100)
	e := running(1, 2, "gke-scaling", app.InstallPhaseVerifying)
	e.Source = "github.com/acme/skills\x1b]0;pwned\x07"
	e.Version = "" // unknown until resolved: must render as —
	m = m.Observe(e)

	view := m.View()
	if !strings.Contains(view, "gke-scaling") {
		t.Errorf("view lacks current skill name:\n%s", view)
	}
	if !strings.Contains(view, "Verifying integrity") {
		t.Errorf("view lacks translated phase:\n%s", view)
	}
	if strings.Contains(view, "\x1b]0;") || strings.Contains(view, "pwned\x07") {
		t.Errorf("view leaked terminal escape sequence:\n%q", view)
	}
	if !strings.Contains(view, "github.com/acme/skills") {
		t.Errorf("view lacks sanitized source:\n%s", view)
	}
	if !strings.Contains(view, "—") {
		t.Errorf("unknown version does not render as —:\n%s", view)
	}
}

func TestInstallProgress_NarrowLayout(t *testing.T) {
	t.Parallel()

	m := NewInstallProgress()
	m = m.Observe(running(1, 5, "alpha", app.InstallPhaseFetching))
	m = m.Observe(term(1, 5, "alpha", app.InstallStatusInstalled))
	m = m.Observe(running(2, 5, "beta", app.InstallPhaseFetching))

	wide := m.SetWidth(100).View()
	if !strings.Contains(wide, "Skill:") {
		t.Errorf("wide layout lacks the Current panel:\n%s", wide)
	}

	narrow := m.SetWidth(59).View()
	if strings.Contains(narrow, "Skill:") {
		t.Errorf("narrow layout (<60 cols) still uses the wide Current panel:\n%s", narrow)
	}
	for _, want := range []string{"beta", "1/5"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("narrow layout lacks %q:\n%s", want, narrow)
		}
	}
}

func TestInstallProgress_EmptyRunNoDivideByZero(t *testing.T) {
	t.Parallel()

	m := NewInstallProgress().SetWidth(80)
	if got := m.Percent(); got != 0 {
		t.Errorf("empty run Percent() = %d, want 0", got)
	}
	view := m.View() // must not panic or render NaN
	if strings.Contains(view, "NaN") {
		t.Errorf("empty run view renders NaN:\n%s", view)
	}
}

func TestInstallProgress_DryRunPlannedEvents(t *testing.T) {
	t.Parallel()

	m := NewInstallProgress().SetWidth(80)
	m = m.Observe(term(1, 2, "a", app.InstallStatusPlanned))
	m = m.Observe(term(2, 2, "b", app.InstallStatusPlanned))
	if got := m.Percent(); got != 100 {
		t.Errorf("planned terminals Percent() = %d, want 100", got)
	}
	if !m.Done() {
		t.Error("Done() = false after all planned terminals")
	}
}

func TestInstallProgress_RunScopedEventsIgnored(t *testing.T) {
	t.Parallel()

	m := NewInstallProgress().SetWidth(80)
	m = m.Observe(term(1, 2, "a", app.InstallStatusInstalled))
	before := m.Percent()
	m = m.Observe(app.InstallProgressEvent{
		SkillTotal: 2, Phase: app.InstallPhaseLocking, Status: app.InstallStatusRunning,
	})
	if got := m.Percent(); got != before {
		t.Errorf("run-scoped event changed Percent() %d -> %d", before, got)
	}
	if strings.Contains(m.View(), "Updating lockfile") && strings.Contains(m.View(), "Skill:") {
		// The run-scoped phase must not masquerade as a current skill.
		t.Errorf("run-scoped locking event replaced the current skill panel:\n%s", m.View())
	}
}
