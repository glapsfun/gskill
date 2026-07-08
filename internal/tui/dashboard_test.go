package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func dashRows() []SkillRow {
	return []SkillRow{
		{Name: "tui-design", Version: "1.0.1", Source: "acme/skills", Status: "installed", Markdown: "# tui-design\n\nGuidance."},
		{Name: "deploy", Version: "0.9.2", Source: "corp/devops", Status: "outdated", Markdown: "# deploy\n\nHelpers."},
	}
}

// dashUpdate drives the dashboard model through messages.
func dashUpdate(t *testing.T, m model, msgs ...tea.Msg) model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		var ok bool
		if m, ok = next.(model); !ok {
			t.Fatalf("model type changed: %T", next)
		}
	}
	return m
}

func TestDashboard_TableAndPreviewAt80x24(t *testing.T) {
	t.Parallel()
	m := dashUpdate(t, newModel(dashRows()), tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	for _, want := range []string{"gskill", "NAME", "VERSION", "SOURCE", "STATUS", "tui-design", "●", "◐", "tui-design 1.0.1"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "─") {
		t.Errorf("view has no border glyphs:\n%s", v)
	}
	// The view must fit the terminal: no more lines than the height.
	if got := strings.Count(v, "\n"); got > 24 {
		t.Errorf("view is %d lines tall at 24 rows:\n%s", got, v)
	}
}

func TestDashboard_PreviewFollowsCursor(t *testing.T) {
	t.Parallel()
	m := dashUpdate(t, newModel(dashRows()), tea.WindowSizeMsg{Width: 80, Height: 24},
		tea.KeyMsg{Type: tea.KeyDown})
	if v := m.View(); !strings.Contains(v, "deploy 0.9.2") {
		t.Errorf("preview title did not follow the cursor:\n%s", v)
	}
}

func TestDashboard_PreviewCollapsesWhenShort(t *testing.T) {
	t.Parallel()
	m := dashUpdate(t, newModel(dashRows()), tea.WindowSizeMsg{Width: 80, Height: 16})
	v := m.View()
	if strings.Contains(v, "tui-design 1.0.1") {
		t.Errorf("preview strip should collapse below 20 rows:\n%s", v)
	}
	if !strings.Contains(v, "NAME") {
		t.Errorf("table must survive the collapse:\n%s", v)
	}
}

func TestDashboard_TabFocusesPreview(t *testing.T) {
	t.Parallel()
	m := dashUpdate(t, newModel(dashRows()), tea.WindowSizeMsg{Width: 80, Height: 24},
		tea.KeyMsg{Type: tea.KeyTab})
	before := m.tbl.Cursor()
	m = dashUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.tbl.Cursor() != before {
		t.Error("down must scroll the focused preview, not move the table cursor")
	}
}

func TestDashboard_EmptyState(t *testing.T) {
	t.Parallel()
	m := dashUpdate(t, newModel(nil), tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	if !strings.Contains(v, "No skills installed.") || !strings.Contains(v, "gskill onboard") {
		t.Errorf("empty state must keep its wording and point at onboarding:\n%s", v)
	}
}
