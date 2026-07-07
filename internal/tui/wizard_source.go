package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The source-input step (US5): entering onboarding without a predefined
// repository. Configured sources are offered as a pick list; below it, the
// user can type a URL, owner/repo shorthand, or local path. The CLI-injected
// validator (source.Parse behind the scenes) rejects bad input inline without
// exiting the flow (FR-002).

// sourceInputModel is a minimal single-line text input.
type sourceInputModel struct {
	value string
}

func newSourceInputModel() sourceInputModel { return sourceInputModel{} }

// handleKey edits the input; printable runes append, backspace trims.
func (s *sourceInputModel) handleKey(key tea.KeyMsg) bool {
	switch key.Type { //nolint:exhaustive // text entry handles only these keys
	case tea.KeyBackspace:
		if r := []rune(s.value); len(r) > 0 {
			s.value = string(r[:len(r)-1])
		}
		return true
	case tea.KeySpace:
		s.value += " "
		return true
	case tea.KeyRunes:
		s.value += string(key.Runes)
		return true
	default:
		return false
	}
}

func (m wizardModel) sourceKey(key tea.KeyMsg) (wizardModel, tea.Cmd, bool) {
	switch key.String() {
	case keyUp, "ctrl+p":
		if m.srcCursor > 0 {
			m.srcCursor--
		}
		return m, nil, true
	case keyDown, "ctrl+n":
		if m.srcCursor < len(m.srcSuggestions) {
			m.srcCursor++
		}
		return m, nil, true
	case keyEnter:
		value := strings.TrimSpace(m.srcInput.value)
		if m.srcCursor < len(m.srcSuggestions) {
			value = m.srcSuggestions[m.srcCursor]
		}
		return m.acceptSource(value)
	}
	// Typing routes to the free-form input row.
	if m.srcInput.handleKey(key) {
		m.srcCursor = len(m.srcSuggestions)
		m.srcErr = ""
		return m, nil, true
	}
	return m, nil, false
}

// acceptSource validates the chosen source and, if valid, starts discovery.
// Validation errors are shown inline; the flow keeps running (US5 scenario 2).
func (m wizardModel) acceptSource(value string) (wizardModel, tea.Cmd, bool) {
	if value == "" {
		m.srcErr = "enter a repository URL, owner/repo shorthand, or local path"
		return m, nil, true
	}
	if m.phases.ValidateSource != nil {
		if err := m.phases.ValidateSource(value); err != nil {
			m.srcErr = err.Error()
			return m, nil, true
		}
	}
	m.srcErr = ""
	m.session.Source = value
	if m.phases.SourceChosen != nil {
		m.phases.SourceChosen(value)
	}
	m.history = append(m.history, m.step)
	m.step = stepWelcome
	m.markWelcomeLoading()
	return m, m.welcomeLoads(), true
}

func (m wizardModel) viewSource() string {
	var b strings.Builder
	b.WriteString(m.header("Where should skills come from?"))
	b.WriteString(m.st.Subtitle.Render("Pick a configured source or type a git URL, owner/repo shorthand, or local path.") + "\n\n")

	for i, s := range m.srcSuggestions {
		cursor := "  "
		label := s
		if i == m.srcCursor {
			cursor = m.st.Cursor.Render("❯") + " "
			label = m.st.Selected.Render(label)
		}
		b.WriteString(cursor + label + "\n")
	}

	inputCursor := "  "
	if m.srcCursor == len(m.srcSuggestions) {
		inputCursor = m.st.Cursor.Render("❯") + " "
	}
	fmt.Fprintf(&b, "%s%s█\n", inputCursor, Sanitize(m.srcInput.value))
	if m.srcErr != "" {
		b.WriteString("\n" + m.st.Error.Render("✗ "+Sanitize(m.srcErr)) + "\n")
	}
	b.WriteString(m.hintLine("↑/↓ move · type to enter a source · enter continue · ctrl+c cancel"))
	return b.String()
}
