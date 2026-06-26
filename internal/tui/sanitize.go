package tui

import (
	"regexp"
	"strings"
)

// ansiRE matches terminal escape sequences: CSI (ESC [ … final), OSC
// (ESC ] … BEL or ST), and other two-byte ESC sequences.
var ansiRE = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

// Sanitize strips terminal control sequences and non-printable control
// characters from untrusted content before it is rendered, defending against
// terminal-injection via remote SKILL.md content (FR-045). Newlines and tabs
// are preserved.
func Sanitize(s string) string {
	s = ansiRE.ReplaceAllString(s, "")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// Drop other C0 controls and DEL (including a lone ESC).
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
