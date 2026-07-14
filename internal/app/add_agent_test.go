package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// TestAddAgentRemainsAdditive (FR-015): gskill add --agent is unaffected by
// this feature's exact-set semantics for `gskill install` — adding an agent
// to an already-installed skill keeps every previously installed agent.
func TestAddAgentRemainsAdditive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"}, Agents: []string{"claude"},
	}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"}, Agents: []string{"codex"},
	}); err != nil {
		t.Fatalf("second add: %v", err)
	}

	for _, marker := range []string{".claude", ".codex"} {
		if _, err := os.Stat(filepath.Join(root, marker, "skills", "demo-skill")); err != nil {
			t.Errorf("%s target missing after additive add: %v", marker, err)
		}
	}
}
