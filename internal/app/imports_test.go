package app_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestAppPackage_NoRenderingImports (spec 014 FR-009, constitution V): the
// domain layer must never depend on terminal rendering — Bubble Tea, Lip
// Gloss, or any charmbracelet package. Events flow up through callbacks;
// rendering happens strictly in internal/tui and internal/cli.
func TestAppPackage_NoRenderingImports(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not available")
	}
	out, err := exec.CommandContext(context.Background(),
		"go", "list", "-deps", "github.com/glapsfun/gskill/internal/app").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, "charmbracelet") {
			t.Errorf("internal/app transitively imports a rendering package: %s", line)
		}
	}
}
