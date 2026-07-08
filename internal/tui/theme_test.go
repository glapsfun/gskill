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
	// Header and Cell must carry the same horizontal padding, or every header
	// renders offset from its column and the data cells lose their gutters.
	if ts.Header.GetPaddingLeft() != ts.Cell.GetPaddingLeft() ||
		ts.Header.GetPaddingRight() != ts.Cell.GetPaddingRight() {
		t.Errorf("header/cell padding mismatch: header (%d,%d) vs cell (%d,%d)",
			ts.Header.GetPaddingLeft(), ts.Header.GetPaddingRight(),
			ts.Cell.GetPaddingLeft(), ts.Cell.GetPaddingRight())
	}
}

func TestHealthCell_Vocabulary(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	cases := []struct{ state, glyph string }{
		{"ok", "●"},
		{"ok-symlink", "●"},
		{"ok-copy", "●"},
		{"mode-mismatch", "◐"},
		{"legacy-store", "◐"},
		{"missing", "✗"},
		{"broken", "✗"},
		{"broken-link", "✗"},
		{"foreign", "✗"},
		{"corrupt", "✗"},
		{"wrong-store-target", "✗"},
	}
	for _, c := range cases {
		got := th.HealthCell(c.state)
		if !strings.Contains(got, c.glyph) || !strings.Contains(got, c.state) {
			t.Errorf("HealthCell(%q) = %q, want glyph %q and the raw state", c.state, got, c.glyph)
		}
	}
	if got := th.HealthCell("someday-state"); got != "someday-state" {
		t.Errorf("unknown state must render plain, got %q", got)
	}
}
