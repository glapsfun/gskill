package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestCursor_DirsAndDetection(t *testing.T) {
	t.Parallel()

	a := agent.NewCursor()
	if a.ID() != "cursor" {
		t.Errorf("ID = %q, want cursor", a.ID())
	}
	if got := a.ProjectSkillDir("/proj"); got != filepath.Join("/proj", ".cursor", "skills") {
		t.Errorf("ProjectSkillDir = %q", got)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".cursor"), 0o750); err != nil {
		t.Fatal(err)
	}
	detected, err := a.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !detected {
		t.Error("cursor not detected with .cursor present")
	}
}

func TestDefaultRegistry_HasAllFourAgents(t *testing.T) {
	t.Parallel()

	reg := agent.NewDefaultRegistry()
	for _, id := range []string{"claude", "codex", "cursor", "gemini-cli"} {
		if _, ok := reg.Get(id); !ok {
			t.Errorf("default registry missing %q", id)
		}
	}
}
