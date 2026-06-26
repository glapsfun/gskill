package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestGeminiCLI_DirsAndDetection(t *testing.T) {
	t.Parallel()

	a := agent.NewGeminiCLI()
	if a.ID() != "gemini-cli" {
		t.Errorf("ID = %q, want gemini-cli", a.ID())
	}
	if got := a.GlobalSkillDir("/home"); got != filepath.Join("/home", ".gemini", "skills") {
		t.Errorf("GlobalSkillDir = %q", got)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gemini"), 0o750); err != nil {
		t.Fatal(err)
	}
	detected, err := a.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !detected {
		t.Error("gemini-cli not detected with .gemini present")
	}
}
