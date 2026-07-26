package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func updItems() []UpdateItem {
	return []UpdateItem{
		{Name: "kubernetes", Current: "1.2.0", Candidate: "1.4.1", Policy: "^1.2.0"},
		{Name: "security", Current: "2.3.0", Candidate: "2.3.2", Policy: "^2.0.0"},
		{Name: "edge", Current: "ffffffffffff", Candidate: "111111111111", Policy: "branch:main"},
	}
}

func manyUpdItems(n int) []UpdateItem {
	out := make([]UpdateItem, n)
	for i := range out {
		out[i] = UpdateItem{
			Name: fmt.Sprintf("skill-%03d", i), Current: "1.0.0", Candidate: "1.1.0", Policy: "^1.0.0",
		}
	}
	return out
}

// driveUpd drives the update selector with messages and returns the model.
func driveUpd(t *testing.T, m tea.Model, msgs ...tea.Msg) updateSelectorModel {
	t.Helper()
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	um, ok := m.(updateSelectorModel)
	if !ok {
		t.Fatalf("model is not updateSelectorModel: %T", m)
	}
	return um
}

// sendUpd drives the update selector with key presses.
func sendUpd(t *testing.T, m tea.Model, keys ...string) updateSelectorModel {
	t.Helper()
	msgs := make([]tea.Msg, len(keys))
	for i, k := range keys {
		msgs[i] = key(k)
	}
	return driveUpd(t, m, msgs...)
}

func TestUpdateSelector_RowsShowVersionMetadata(t *testing.T) {
	t.Parallel()

	view := newUpdateSelectorModel(updItems()).View()
	if !strings.Contains(view, "Select updates") {
		t.Errorf("header missing:\n%s", view)
	}
	for _, want := range []string{
		"kubernetes", "1.2.0", "1.4.1", "^1.2.0",
		"security", "2.3.0", "2.3.2", "^2.0.0",
		"branch:main", "->",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestUpdateSelector_ToggleAndConfirmReturnsChosenNames(t *testing.T) {
	t.Parallel()

	m := sendUpd(t, newUpdateSelectorModel(updItems()), " ", "down", "down", " ", "enter")
	if !m.done {
		t.Fatal("enter did not confirm")
	}
	sel := m.selection()
	if got, want := strings.Join(sel.Names, ","), "kubernetes,edge"; got != want {
		t.Errorf("selection = %q, want %q", got, want)
	}
	if sel.Cancelled || sel.Interrupted {
		t.Errorf("confirmed selection flagged cancelled: %+v", sel)
	}
}

func TestUpdateSelector_FilterSelectionKeyedByOriginalIndex(t *testing.T) {
	t.Parallel()

	// Filter down to security, toggle it, clear the filter, confirm: the
	// toggle must stick to the original item, not the visible position.
	m := sendUpd(t, newUpdateSelectorModel(updItems()), "/", "s", "e", "c", " ", "esc", "esc", "enter")
	sel := m.selection()
	if got, want := strings.Join(sel.Names, ","), "security"; got != want {
		t.Errorf("selection = %q, want %q", got, want)
	}
}

func TestUpdateSelector_DeselectWorks(t *testing.T) {
	t.Parallel()

	m := sendUpd(t, newUpdateSelectorModel(updItems()), " ", " ", "enter")
	if got := m.selection().Names; len(got) != 0 {
		t.Errorf("toggle twice left %v selected, want none", got)
	}
}

func TestUpdateSelector_EscCancelsWithoutSelection(t *testing.T) {
	t.Parallel()

	m := sendUpd(t, newUpdateSelectorModel(updItems()), " ", "esc")
	sel := m.selection()
	if !sel.Cancelled || sel.Interrupted || len(sel.Names) != 0 {
		t.Errorf("esc: selection = %+v, want cancelled with no names", sel)
	}
}

func TestUpdateSelector_CtrlCInterrupts(t *testing.T) {
	t.Parallel()

	m := driveUpd(t, newUpdateSelectorModel(updItems()), tea.KeyMsg{Type: tea.KeyCtrlC})
	sel := m.selection()
	if !sel.Interrupted || len(sel.Names) != 0 {
		t.Errorf("ctrl-c: selection = %+v, want interrupted with no names", sel)
	}
}

func TestUpdateSelector_EmptyListRendersTerminalState(t *testing.T) {
	t.Parallel()

	view := newUpdateSelectorModel(nil).View()
	if !strings.Contains(view, "no updates available") {
		t.Errorf("empty selector missing terminal-state message:\n%s", view)
	}
}

func TestUpdateSelector_NarrowWidthStaysReadable(t *testing.T) {
	t.Parallel()

	m := driveUpd(t, newUpdateSelectorModel(updItems()), win(24, 12))
	for _, line := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(line); w > 24 {
			t.Errorf("line wider than terminal (%d > 24): %q", w, line)
		}
	}
}

func TestUpdateSelector_SmallHeightBoundsWindow(t *testing.T) {
	t.Parallel()

	m := driveUpd(t, newUpdateSelectorModel(manyUpdItems(30)), win(80, 10))
	view := m.View()
	if !strings.Contains(view, "↓ more") {
		t.Errorf("bounded window missing overflow marker:\n%s", view)
	}
	if rows := strings.Count(view, "[ ]"); rows > 10 {
		t.Errorf("%d rows rendered in a 10-line terminal", rows)
	}
}
