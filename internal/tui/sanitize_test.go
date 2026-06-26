package tui_test

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/tui"
)

func TestSanitize_StripsEscapesKeepsText(t *testing.T) {
	t.Parallel()

	in := "\x1b[31mred\x1b[0m text\nsecond\tline\n"
	got := tui.Sanitize(in)
	want := "red text\nsecond\tline\n"
	if got != want {
		t.Errorf("Sanitize = %q, want %q", got, want)
	}
}

func TestSanitize_RemovesOSCTitleInjection(t *testing.T) {
	t.Parallel()

	in := "before\x1b]0;pwned\x07after"
	got := tui.Sanitize(in)
	if strings.Contains(got, "pwned") || strings.Contains(got, "\x1b") {
		t.Errorf("OSC injection survived sanitization: %q", got)
	}
	if got != "beforeafter" {
		t.Errorf("Sanitize = %q, want %q", got, "beforeafter")
	}
}

func TestSanitize_DropsControlChars(t *testing.T) {
	t.Parallel()

	in := "a\x07b\x00c\x1bd"
	got := tui.Sanitize(in)
	if strings.ContainsAny(got, "\x00\x07\x1b") {
		t.Errorf("control characters survived: %q", got)
	}
}
