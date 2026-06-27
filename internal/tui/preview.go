package tui

import (
	"fmt"

	"github.com/charmbracelet/glamour"
)

const defaultWidth = 80

// Preview renders SKILL.md markdown for terminal display, sanitizing untrusted
// escape sequences before rendering so remote content cannot inject terminal
// control codes (FR-045).
func Preview(markdown string, width int) (string, error) {
	clean := Sanitize(markdown)
	if width <= 0 {
		width = defaultWidth
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("notty"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", fmt.Errorf("create renderer: %w", err)
	}
	out, err := renderer.Render(clean)
	if err != nil {
		return "", fmt.Errorf("render preview: %w", err)
	}
	return out, nil
}
