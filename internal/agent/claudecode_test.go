package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestClaudeCode_DirsAndSymlinks(t *testing.T) {
	t.Parallel()

	a := agent.NewClaudeCode()
	if a.ID() != "claude" {
		t.Errorf("ID = %q, want claude", a.ID())
	}
	if got := a.ProjectSkillDir("/proj"); got != filepath.Join("/proj", ".claude", "skills") {
		t.Errorf("ProjectSkillDir = %q", got)
	}
	if got := a.GlobalSkillDir("/home"); got != filepath.Join("/home", ".claude", "skills") {
		t.Errorf("GlobalSkillDir = %q", got)
	}
	if !a.SupportsSymlinks() {
		t.Error("SupportsSymlinks = false, want true")
	}
}

func TestClaudeCode_DetectsMarkerDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := agent.NewClaudeCode()

	detected, err := a.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if detected {
		t.Error("detected with no .claude dir present")
	}

	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	detected, err = a.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !detected {
		t.Error("not detected with .claude dir present")
	}
}

func TestClaudeCode_ValidateInstallation(t *testing.T) {
	t.Parallel()

	a := agent.NewClaudeCode()
	dir := t.TempDir()

	if err := a.ValidateInstallation(context.Background(), dir); err == nil {
		t.Error("validation passed without SKILL.md")
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ValidateInstallation(context.Background(), dir); err != nil {
		t.Errorf("validation failed with SKILL.md present: %v", err)
	}
}

func TestNewDefaultRegistry_HasClaudeAndCodex(t *testing.T) {
	t.Parallel()

	reg := agent.NewDefaultRegistry()
	for _, id := range []string{"claude", "codex"} {
		if _, ok := reg.Get(id); !ok {
			t.Errorf("default registry missing %q", id)
		}
	}
}
