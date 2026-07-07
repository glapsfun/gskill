package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The source-input step (US5): entering onboarding without a predefined
// repository. The user types a URL, owner/repo shorthand, or local path; the
// CLI-injected validator (source.Parse behind the scenes) rejects bad input
// inline without exiting the flow (FR-002).

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
	if key.String() == keyEnter {
		value := strings.TrimSpace(m.srcInput.value)
		if value == "" {
			m.srcErr = "enter a repository URL, owner/repo shorthand, or local path"
			return m, nil, true
		}
		if m.phases.ValidateSource != nil {
			if err := m.phases.ValidateSource(value); err != nil {
				m.srcErr = err.Error() // shown inline; the flow keeps running (US5 scenario 2)
				return m, nil, true
			}
		}
		m.srcErr = ""
		m.session.Source = value
		m.history = append(m.history, m.step)
		m.step = stepWelcome
		return m, m.startDiscover(), true
	}
	if m.srcInput.handleKey(key) {
		m.srcErr = ""
		return m, nil, true
	}
	return m, nil, false
}

func (m wizardModel) viewSource() string {
	var b strings.Builder
	b.WriteString(m.header("Where should skills come from?"))
	b.WriteString(m.st.Subtitle.Render("A git URL, owner/repo shorthand, or local path.") + "\n\n")
	fmt.Fprintf(&b, "  %s %s█\n", m.st.Cursor.Render("❯"), Sanitize(m.srcInput.value))
	if m.srcErr != "" {
		b.WriteString("\n" + m.st.Error.Render("✗ "+Sanitize(m.srcErr)) + "\n")
	}
	b.WriteString(m.hintLine("enter continue · ctrl+c cancel"))
	return b.String()
}
