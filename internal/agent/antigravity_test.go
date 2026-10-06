package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestAntigravity_DirsAndIdentity(t *testing.T) {
	t.Parallel()

	a := agent.NewAntigravity()
	if a.ID() != "antigravity" {
		t.Errorf("ID = %q, want antigravity", a.ID())
	}
	if a.DisplayName() != "Antigravity CLI" {
		t.Errorf("DisplayName = %q, want Antigravity CLI", a.DisplayName())
	}
	if got, want := a.ProjectSkillDir("/proj"), filepath.Join("/proj", ".agents", "skills"); got != want {
		t.Errorf("ProjectSkillDir = %q, want %q", got, want)
	}
	if got, want := a.GlobalSkillDir("/home"), filepath.Join("/home", ".gemini", "antigravity-cli", "skills"); got != want {
		t.Errorf("GlobalSkillDir = %q, want %q", got, want)
	}
	if !a.SupportsSymlinks() {
		t.Error("SupportsSymlinks = false, want true")
	}
}

func TestAntigravity_NeverDetected(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, d := range []string{".gemini", ".agents"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	detected, err := agent.NewAntigravity().Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if detected {
		t.Error("antigravity detected; it has no project marker and must be targeted explicitly")
	}
}
