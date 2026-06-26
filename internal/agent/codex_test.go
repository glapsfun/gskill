package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestCodex_DirsAndDetection(t *testing.T) {
	t.Parallel()

	a := agent.NewCodex()
	if a.ID() != "codex" {
		t.Errorf("ID = %q, want codex", a.ID())
	}
	if got := a.ProjectSkillDir("/proj"); got != filepath.Join("/proj", ".codex", "skills") {
		t.Errorf("ProjectSkillDir = %q", got)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o750); err != nil {
		t.Fatal(err)
	}
	detected, err := a.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !detected {
		t.Error("codex not detected with .codex dir present")
	}
}
