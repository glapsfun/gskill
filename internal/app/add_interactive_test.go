package app_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
)

// projectWithAgent creates an initialized project with a .claude marker so an
// agent is detected.
func projectWithAgent(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	a := app.New(app.Options{Agents: agent.NewDefaultRegistry(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAddInteractive_ChooserInstallsSubset(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/a", "skills/b", "skills/c")
	proj := projectWithAgent(t)
	a := app.New(app.Options{Agents: agent.NewDefaultRegistry(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	var sawCandidates int
	chooser := func(cands []discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error) {
		sawCandidates = len(cands)
		// Pick just "b".
		for _, c := range cands {
			if c.ID == "b" {
				return []discovery.DiscoveredSkill{c}, nil
			}
		}
		return nil, nil
	}

	res, err := a.Add(context.Background(), app.AddRequest{
		Root: proj, Source: src, Interactive: true, Chooser: chooser,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sawCandidates != 3 {
		t.Errorf("chooser saw %d candidates, want 3", sawCandidates)
	}
	if len(res.Installed) != 1 || res.Installed[0].Name != "b" {
		t.Errorf("installed = %+v, want only b", res.Installed)
	}
}

func TestAddInteractive_FailsFastWithoutAgentBeforeChooser(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/a", "skills/b")
	proj := t.TempDir() // no agent marker
	// Registry without the default (claude) and no detected agent, so agent
	// resolution fails before any interactive selection.
	reg := agent.NewRegistry()
	if err := reg.Register(agent.NewCodex()); err != nil {
		t.Fatal(err)
	}
	a := app.New(app.Options{Agents: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := a.Init(context.Background(), proj, false); err != nil {
		t.Fatal(err)
	}

	chooserCalled := false
	chooser := func(cands []discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error) {
		chooserCalled = true
		return cands, nil
	}

	_, err := a.Add(context.Background(), app.AddRequest{
		Root: proj, Source: src, Interactive: true, Chooser: chooser,
	})
	if err == nil {
		t.Fatal("expected a no-agent error")
	}
	if chooserCalled {
		t.Error("interactive picker must not run when no agent is available (fail fast)")
	}
}
