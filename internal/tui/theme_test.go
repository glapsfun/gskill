package tui

import (
	"strings"
	"testing"
)

func TestStatusCell_Vocabulary(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	cases := []struct{ status, glyph string }{
		{"installed", "●"},
		{"modified", "◐"},
		{"outdated", "◐"},
		{"partially-installed", "◐"},
		{"missing", "✗"},
		{"orphaned", "✗"},
		{"source-unavailable", "✗"},
		{"checksum-mismatch", "✗"},
		{"manifest-lock-mismatch", "✗"},
	}
	for _, c := range cases {
		got := th.StatusCell(c.status)
		if !strings.Contains(got, c.glyph) || !strings.Contains(got, c.status) {
			t.Errorf("StatusCell(%q) = %q, want glyph %q and the raw status", c.status, got, c.glyph)
		}
	}
	if got := th.StatusCell("someday-status"); got != "someday-status" {
		t.Errorf("unknown status must render plain, got %q", got)
	}
}

func TestThemeAdapters_NotNil(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	if th.Huh() == nil {
		t.Fatal("Huh() returned nil")
	}
	// TableStyles must carry the theme, not bubbles defaults: the selected row
	// uses a foreground accent and the header is bold.
	ts := th.TableStyles()
	if !ts.Selected.GetBold() {
		t.Error("Selected style must be the bold accent, not the default highlight")
	}
	if !ts.Header.GetBold() {
		t.Error("Header style must be bold")
	}
}
