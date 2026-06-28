package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/glapsfun/gskill/internal/errs"
)

// SkillItem is one selectable skill in the interactive picker.
type SkillItem struct {
	ID          string
	DisplayName string
	RepoPath    string
	Valid       bool
}

// SelectSkills runs the interactive multi-select picker and returns the indices
// of the chosen skills. It refuses to start without a TTY. A cancelled picker
// returns an empty selection and a nil error (the caller treats that as "no
// skill selected").
func SelectSkills(items []SkillItem, isTTY bool) ([]int, error) {
	if !isTTY {
		return nil, fmt.Errorf("%w: interactive selection requires a TTY; pass --skill, --all, or --list", errs.ErrUsage)
	}
	final, err := tea.NewProgram(newSelectorModel(items)).Run()
	if err != nil {
		return nil, fmt.Errorf("tui: %w", err)
	}
	m, ok := final.(selectorModel)
	if !ok || m.cancelled {
		return nil, nil
	}
	return m.chosenIndices(), nil
}

// selectorModel is the multi-select picker state.
type selectorModel struct {
	items     []SkillItem
	cursor    int
	chosen    map[int]bool
	done      bool
	cancelled bool
}

func newSelectorModel(items []SkillItem) selectorModel {
	return selectorModel{items: items, chosen: make(map[int]bool)}
}

// Init implements tea.Model.
func (m selectorModel) Init() tea.Cmd { return nil }

// Update implements tea.Model: arrows move, space toggles a valid item, enter
// confirms, q/esc/ctrl+c cancels.
func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case " ", "x":
		if m.cursor < len(m.items) && m.items[m.cursor].Valid {
			m.chosen[m.cursor] = !m.chosen[m.cursor]
		}
	case "enter":
		m.done = true
		return m, tea.Quit
	case "q", "esc", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m selectorModel) View() string {
	var b strings.Builder
	b.WriteString("Select skills to install (space to toggle, enter to confirm, q to cancel):\n\n")
	for i, it := range m.items {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		check := "[ ]"
		if m.chosen[i] {
			check = "[x]"
		}
		suffix := ""
		if !it.Valid {
			check = "[-]"
			suffix = " (invalid)"
		}
		path := it.RepoPath
		if path == "" {
			path = "."
		}
		fmt.Fprintf(&b, "%s %s %s  %s%s\n", cursor, check, it.ID, path, suffix)
	}
	return b.String()
}

// chosenIndices returns the indices of selected valid items in order.
func (m selectorModel) chosenIndices() []int {
	var out []int
	for i := range m.items {
		if m.chosen[i] && m.items[i].Valid {
			out = append(out, i)
		}
	}
	return out
}
