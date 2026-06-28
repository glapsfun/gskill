package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(t *testing.T, m tea.Model, keys ...string) selectorModel {
	t.Helper()
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	sm, ok := m.(selectorModel)
	if !ok {
		t.Fatalf("model is not selectorModel: %T", m)
	}
	return sm
}

func items() []SkillItem {
	return []SkillItem{
		{ID: "code-review", RepoPath: "skills/code-review", Valid: true},
		{ID: "writing", RepoPath: "skills/writing", Valid: true},
		{ID: "broken", RepoPath: "skills/broken", Valid: false},
	}
}

func TestSelector_ToggleAndConfirm(t *testing.T) {
	t.Parallel()

	// Toggle first item, move down, toggle second, confirm.
	m := send(t, newSelectorModel(items()), " ", "down", " ", "enter")
	if !m.done {
		t.Fatal("expected done after enter")
	}
	got := m.chosenIndices()
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("chosen = %v, want [0 1]", got)
	}
}

func TestSelector_InvalidNotSelectable(t *testing.T) {
	t.Parallel()

	// Move to the invalid item (index 2) and try to toggle it.
	m := send(t, newSelectorModel(items()), "down", "down", " ", "enter")
	if len(m.chosenIndices()) != 0 {
		t.Errorf("invalid item must not be selectable, chose %v", m.chosenIndices())
	}
}

func TestSelector_Cancel(t *testing.T) {
	t.Parallel()

	m := send(t, newSelectorModel(items()), " ", "esc")
	if !m.cancelled {
		t.Error("expected cancelled after esc")
	}
}

func TestSelector_DuplicateBothSelectable(t *testing.T) {
	t.Parallel()

	// Two skills with the same id but different paths — both valid, both shown,
	// both individually selectable (FR-024 interactive branch).
	dups := []SkillItem{
		{ID: "shared", RepoPath: "skills/a/shared", Valid: true},
		{ID: "shared", RepoPath: "skills/b/shared", Valid: true},
	}
	m := send(t, newSelectorModel(dups), " ", "down", " ", "enter")
	if got := m.chosenIndices(); len(got) != 2 {
		t.Errorf("both duplicate paths should be selectable, chose %v", got)
	}
}
