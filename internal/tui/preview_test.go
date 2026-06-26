package tui_test

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/tui"
)

func TestPreview_SanitizesBeforeRender(t *testing.T) {
	t.Parallel()

	md := "# Title\n\nbefore\x1b]0;pwned\x07after\n"
	out, err := tui.Preview(md, 80)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	if strings.Contains(out, "pwned") {
		t.Errorf("injected OSC payload survived into preview: %q", out)
	}
	if strings.Contains(out, "\x1b]") {
		t.Errorf("OSC escape survived into preview: %q", out)
	}
	if !strings.Contains(out, "Title") {
		t.Errorf("markdown heading not rendered: %q", out)
	}
}

func TestPreview_RendersBody(t *testing.T) {
	t.Parallel()

	out, err := tui.Preview("# Heading\n\nSome instructions.\n", 80)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !strings.Contains(out, "Some instructions.") {
		t.Errorf("body not rendered: %q", out)
	}
}
