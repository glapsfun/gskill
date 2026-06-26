package tui_test

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/tui"
)

func TestRun_RefusesWithoutTTY(t *testing.T) {
	t.Parallel()

	err := tui.Run([]tui.SkillRow{{Name: "demo", Status: "installed"}}, false)
	if err == nil {
		t.Fatal("Run without a TTY succeeded, want a guard error")
	}
	if errs.ExitCode(err) != 2 {
		t.Errorf("exit code = %d, want 2 (usage)", errs.ExitCode(err))
	}
	if !strings.Contains(err.Error(), "list") && !strings.Contains(err.Error(), "info") {
		t.Errorf("guard error should hint at CLI commands: %v", err)
	}
}
