package tui

import (
	"context"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// newDash builds a model backed by fs, loaded with its inventory, sized to
// w×h. It drives the real async path: Init's inventory load is delivered as an
// inventoryMsg.
func newDash(t *testing.T, fs *fakeService, w, h int) model {
	t.Helper()
	m := newModel(context.Background(), fs, Options{})
	skills, _ := fs.Inventory(context.Background())
	return dashUpdate(t, m, inventoryMsg{skills: skills}, tea.WindowSizeMsg{Width: w, Height: h})
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

func dashKey(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// --- US1: browse, search, details, states (preserves FR-023 read-only behavior) ---

func TestDashboard_GroupedBrowseAt80x24(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	v := m.View()
	for _, want := range []string{"gskill", "NAME", "VERSION", "SOURCE", "STATUS", "tui-design", "deploy", "acme/skills", "corp/devops", "●", "◐"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestDashboard_DetailsShowPolicyAndAgents(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	v := m.View()
	// Selection starts on the first group-ordered skill (acme/skills: tui-design).
	if !strings.Contains(v, "policy ^1.0.0") {
		t.Errorf("details missing tracking policy:\n%s", v)
	}
	if !strings.Contains(v, "agents claude") {
		t.Errorf("details missing configured agents:\n%s", v)
	}
}

func TestDashboard_PreviewFollowsCursor(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	if sel, _ := m.selectedSkill(); sel.Name != "tui-design" {
		t.Fatalf("initial selection = %q, want tui-design", sel.Name)
	}
	m = dashUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if sel, _ := m.selectedSkill(); sel.Name != "deploy" {
		t.Fatalf("after down, selection = %q, want deploy", sel.Name)
	}
	if !strings.Contains(m.View(), "deploy 0.9.2") {
		t.Errorf("preview title did not follow cursor to deploy:\n%s", m.View())
	}
}

func TestDashboard_TabFocusesPreview(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	m = dashUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.focusPreview {
		t.Error("tab did not focus the preview")
	}
}

func TestDashboard_PreviewCollapsesWhenShort(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 16)
	if m.showPreview() {
		t.Error("preview should be hidden below the height floor")
	}
}

func TestDashboard_SearchFiltersCaseInsensitively(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	m = dashUpdate(t, m, dashKey("/"))                             // enter search
	m = dashUpdate(t, m, dashKey("D"), dashKey("e"), dashKey("p")) // type "Dep" (case-insensitive)
	if m.mode != modeSearch {
		t.Fatal("expected search mode")
	}
	if len(m.visible) != 1 || m.skills[m.visible[0]].Name != "deploy" {
		t.Fatalf("filter 'Dep' did not narrow to deploy: visible=%d", len(m.visible))
	}
	v := m.View()
	if strings.Contains(v, "tui-design") {
		t.Errorf("filtered view still shows tui-design:\n%s", v)
	}
}

func TestDashboard_SearchMatchesSourceRepository(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	m = dashUpdate(t, m, dashKey("/"), dashKey("c"), dashKey("o"), dashKey("r"), dashKey("p"))
	if len(m.visible) != 1 || m.skills[m.visible[0]].Name != "deploy" {
		t.Fatalf("repo search 'corp' did not select deploy's group: visible=%d", len(m.visible))
	}
}

func TestDashboard_ClearSearchRestoresInventory(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	m = dashUpdate(t, m, dashKey("/"), dashKey("z")) // no matches
	if len(m.visible) != 0 {
		t.Fatalf("query 'z' should match nothing, got %d", len(m.visible))
	}
	m = dashUpdate(t, m, dashKey("esc")) // clear
	if m.mode != modeBrowse || len(m.visible) != 2 {
		t.Fatalf("esc did not restore full inventory: mode=%d visible=%d", m.mode, len(m.visible))
	}
}

func TestDashboard_NoMatchesState(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	m = dashUpdate(t, m, dashKey("/"), dashKey("z"), dashKey("z"), dashKey("z"))
	v := m.View()
	if !strings.Contains(v, "No skills match") {
		t.Errorf("missing distinct no-matches state:\n%s", v)
	}
}

func TestDashboard_EmptyInventoryState(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: nil}, 80, 24)
	v := m.View()
	if !strings.Contains(v, "No skills installed") {
		t.Errorf("missing empty-inventory state:\n%s", v)
	}
	if strings.Contains(v, "No skills match") {
		t.Errorf("empty inventory confused with no-matches:\n%s", v)
	}
}

func TestDashboard_LoadErrorState(t *testing.T) {
	t.Parallel()
	m := newModel(context.Background(), &fakeService{invErr: errFake}, Options{})
	m = dashUpdate(t, m, dashErrMsg{err: errFake}, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(m.View(), "Could not load skills") {
		t.Errorf("missing load-error state:\n%s", m.View())
	}
}

// TestDashboard_TableCellsCarryNoANSI locks the bubbles/table constraint: the
// library truncates cells by byte width, so a styled cell would be cut mid
// escape sequence and bleed color across the table. A renderer-scoped truecolor
// profile forces the theme to emit ANSI (the test environment's global profile
// is colorless) without mutating the process-global profile that parallel tests
// share.
func TestDashboard_TableCellsCarryNoANSI(t *testing.T) {
	t.Parallel()
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	m.st.Success = r.NewStyle().Foreground(lipgloss.Color("#3FB950"))
	m.st.Warning = r.NewStyle().Foreground(lipgloss.Color("#D29922"))
	if !strings.ContainsRune(m.st.StatusCell("installed"), '\x1b') {
		t.Fatal("test theme renders no ANSI; the assertion below would be vacuous")
	}
	for _, row := range m.visibleRows() {
		for _, cell := range row {
			if strings.ContainsRune(cell, '\x1b') {
				t.Errorf("table cell carries ANSI: %q", cell)
			}
		}
	}
}

func TestDashboard_FooterListsActions(t *testing.T) {
	t.Parallel()
	m := newDash(t, &fakeService{skills: dashSkills()}, 80, 24)
	if !strings.Contains(m.View(), "search") {
		t.Errorf("footer does not list the search action:\n%s", m.View())
	}
}
