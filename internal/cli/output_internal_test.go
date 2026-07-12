package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/glapsfun/gskill/internal/tui"
)

func TestStyleDiag_IdentityWithoutColor(t *testing.T) {
	t.Parallel()

	st := tui.DefaultTheme().Warning
	if got := styleDiag(false, st, "warning: x"); got != "warning: x" {
		t.Errorf("styleDiag(false, ...) = %q, want unchanged text", got)
	}
}

// TestStyleDiag_AppliesStyleWhenInteractive forces a renderer-scoped
// truecolor profile: the test suite's global lipgloss profile is colorless,
// so proving color requires bypassing it the same way
// TestDashboard_TableCellsCarryNoANSI (internal/tui/dashboard_test.go) does.
func TestStyleDiag_AppliesStyleWhenInteractive(t *testing.T) {
	t.Parallel()

	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	st := r.NewStyle().Foreground(lipgloss.Color("#D29922"))

	got := styleDiag(true, st, "warning: x")
	if !strings.Contains(got, "\x1b") {
		t.Fatalf("styleDiag did not apply color when interactive: %q", got)
	}
	if stripped := stripAnsi(got); stripped != "warning: x" {
		t.Errorf("styled text lost content: got %q, want %q", stripped, "warning: x")
	}
}

// TestOutput_StderrColor_RequiresBothInteractiveAndStderrTTY locks the fix
// for stdout/stderr TTY-status confusion: o.interactive alone answers "is
// stdout a TTY" (NewOutput's own definition, correct for stdout-bound
// rendering) and says nothing about stderr, so every stderr-styling call
// site must additionally check isTTY(o.stderr) itself. A bytes.Buffer is
// never a TTY, so this proves the AND — the true/true branch (an actual
// terminal) is proven separately by TestStyleDiag_AppliesStyleWhenInteractive
// forcing color on the underlying styleDiag call.
func TestOutput_StderrColor_RequiresBothInteractiveAndStderrTTY(t *testing.T) {
	t.Parallel()

	o := &Output{interactive: true, stderr: &bytes.Buffer{}}
	if o.stderrColor() {
		t.Error("stderrColor() = true with non-TTY stderr, want false")
	}

	o2 := &Output{interactive: false, stderr: &bytes.Buffer{}}
	if o2.stderrColor() {
		t.Error("stderrColor() = true with interactive=false, want false")
	}
}
