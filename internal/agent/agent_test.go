package agent_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

// fakeAgent is a minimal Agent for exercising the registry.
type fakeAgent struct {
	id       string
	detected bool
}

func (f fakeAgent) ID() string                         { return f.id }
func (f fakeAgent) DisplayName() string                { return f.id }
func (f fakeAgent) ProjectSkillDir(root string) string { return filepath.Join(root, f.id) }
func (f fakeAgent) GlobalSkillDir(home string) string  { return filepath.Join(home, f.id) }
func (f fakeAgent) SupportsSymlinks() bool             { return true }
func (f fakeAgent) Detect(_ context.Context, _ string) (bool, error) {
	return f.detected, nil
}
func (f fakeAgent) ValidateInstallation(_ context.Context, _ string) error { return nil }

func TestRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()

	r := agent.NewRegistry()
	if err := r.Register(fakeAgent{id: "claude"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Get("claude")
	if !ok {
		t.Fatal("Get(claude) not found")
	}
	if got.ID() != "claude" {
		t.Errorf("ID = %q, want %q", got.ID(), "claude")
	}

	if _, ok := r.Get("absent"); ok {
		t.Error("Get(absent) reported found")
	}
}

func TestRegistry_DuplicateRegistrationErrors(t *testing.T) {
	t.Parallel()

	r := agent.NewRegistry()
	if err := r.Register(fakeAgent{id: "codex"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(fakeAgent{id: "codex"}); err == nil {
		t.Error("duplicate Register succeeded, want error")
	}
}

func TestRegistry_AllPreservesRegistrationOrder(t *testing.T) {
	t.Parallel()

	r := agent.NewRegistry()
	ids := []string{"claude", "codex", "cursor", "gemini-cli"}
	for _, id := range ids {
		if err := r.Register(fakeAgent{id: id}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}

	var got []string
	for _, a := range r.All() {
		got = append(got, a.ID())
	}
	if len(got) != len(ids) {
		t.Fatalf("All() returned %d agents, want %d", len(got), len(ids))
	}
	for i := range ids {
		if got[i] != ids[i] {
			t.Errorf("All()[%d] = %q, want %q", i, got[i], ids[i])
		}
	}
}

func TestRegistry_DetectFiltersToDetectedAgents(t *testing.T) {
	t.Parallel()

	r := agent.NewRegistry()
	_ = r.Register(fakeAgent{id: "claude", detected: true})
	_ = r.Register(fakeAgent{id: "codex", detected: false})
	_ = r.Register(fakeAgent{id: "cursor", detected: true})

	detected, err := r.Detect(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(detected) != 2 {
		t.Fatalf("Detect returned %d agents, want 2", len(detected))
	}
	if detected[0].ID() != "claude" || detected[1].ID() != "cursor" {
		t.Errorf("Detect order = [%s %s], want [claude cursor]", detected[0].ID(), detected[1].ID())
	}
}

func TestRegistry_RegisterRejectsEmptyID(t *testing.T) {
	t.Parallel()

	r := agent.NewRegistry()
	if err := r.Register(fakeAgent{id: ""}); err == nil {
		t.Error("Register with empty ID succeeded, want error")
	} else if !errors.Is(err, agent.ErrInvalidAgent) {
		t.Errorf("error = %v, want ErrInvalidAgent", err)
	}
}
