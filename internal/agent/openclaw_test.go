package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestOpenClaw_DirsAndIdentity(t *testing.T) {
	t.Parallel()

	a := agent.NewOpenClaw()
	if a.ID() != "openclaw" {
		t.Errorf("ID = %q, want openclaw", a.ID())
	}
	if a.DisplayName() != "OpenClaw" {
		t.Errorf("DisplayName = %q, want OpenClaw", a.DisplayName())
	}
	if !agent.UsesSharedDir(a, "/proj") {
		t.Error("OpenClaw must read the shared store")
	}
	if got, want := a.GlobalSkillDir("/home"), filepath.Join("/home", ".openclaw", "skills"); got != want {
		t.Errorf("GlobalSkillDir = %q, want %q", got, want)
	}
}

func TestOpenClaw_NeverDetected(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, d := range []string{".openclaw", ".agents"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	detected, err := agent.NewOpenClaw().Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if detected {
		t.Error("openclaw detected; it has no project marker and must be targeted explicitly")
	}
}
