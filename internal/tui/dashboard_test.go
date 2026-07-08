package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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

// TestDashboard_TableCellsCarryNoANSI locks the bubbles/table constraint: the
// library truncates cells with a width function that counts escape bytes, so
// a styled cell would be cut mid-sequence and bleed color across the table.
// A renderer-scoped truecolor profile forces the theme to actually emit ANSI
// (the test environment's global profile is colorless).
func TestDashboard_TableCellsCarryNoANSI(t *testing.T) {
	t.Parallel()
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	st := DefaultTheme()
	st.Success = r.NewStyle().Foreground(lipgloss.Color("#3FB950"))
	st.Warning = r.NewStyle().Foreground(lipgloss.Color("#D29922"))
	if !strings.Contains(st.StatusCell("installed"), "\x1b") {
		t.Fatal("test theme renders no ANSI; the assertion below would be vacuous")
	}
	for _, row := range dashRowsData(dashRows(), st) {
		for _, cell := range row {
			if strings.Contains(cell, "\x1b") {
				t.Errorf("table cell carries ANSI (bubbles/table will mangle it): %q", cell)
			}
		}
	}
}

// TestDashboard_CollapseReleasesPreviewFocus: shrinking the terminal hides the
// preview strip; keys must return to the table instead of scrolling the
// now-invisible viewport.
func TestDashboard_CollapseReleasesPreviewFocus(t *testing.T) {
	t.Parallel()
	m := dashUpdate(t, newModel(dashRows()), tea.WindowSizeMsg{Width: 80, Height: 24},
		tea.KeyMsg{Type: tea.KeyTab}) // focus the preview
	m = dashUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 16}) // preview collapses
	if m.focusPreview {
		t.Fatal("focusPreview still set after the preview collapsed")
	}
	before := m.tbl.Cursor()
	m = dashUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.tbl.Cursor() == before {
		t.Error("down must move the table cursor once the preview is hidden")
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
