package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// TestList_HealthyChain_PopulatesActiveAndAgentHealth verifies that App.List
// carries the same active-layer and per-agent health data App.Status used to
// carry (spec 013 FR-001/FR-005).
func TestList_HealthyChain_PopulatesActiveAndAgentHealth(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	linkAgent(t, root, "demo")

	lf := lockWith("demo", hash)
	if err := saveLock(p.lockPath, lf); err != nil {
		t.Fatalf("saveLock: %v", err)
	}

	got, err := a.List(context.Background(), root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1", len(got))
	}
	skill := got[0]
	if skill.Active != string(active.HealthOK) {
		t.Errorf("Active = %q, want %q", skill.Active, active.HealthOK)
	}
	want := []AgentHealthEntry{{ID: "claude", Mode: "symlink", Health: string(TargetOKSymlink)}}
	if len(skill.AgentHealth) != len(want) || skill.AgentHealth[0] != want[0] {
		t.Errorf("AgentHealth = %+v, want %+v", skill.AgentHealth, want)
	}
}

// TestList_NoAgentTargets_AgentHealthIsEmptyNotNil verifies a skill with no
// agent targets gets an empty (not nil) AgentHealth slice, matching the
// existing `agents: []` convention (spec 013 FR-005, US2 acceptance scenario 2).
func TestList_NoAgentTargets_AgentHealthIsEmptyNotNil(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, _ := seedStore(t, p)
	lf := skillslock.NewState()
	lf.Skills["demo"] = skillslock.Record{
		Resolved: skillslock.Resolved{ContentHash: hash},
		Installation: skillslock.Installation{
			Scope:      "project",
			ActivePath: active.Rel("demo"),
		},
	}
	if err := saveLock(p.lockPath, lf); err != nil {
		t.Fatalf("saveLock: %v", err)
	}

	got, err := a.List(context.Background(), root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1", len(got))
	}
	if got[0].AgentHealth == nil {
		t.Error("AgentHealth = nil, want empty slice")
	}
	if len(got[0].AgentHealth) != 0 {
		t.Errorf("AgentHealth = %+v, want empty", got[0].AgentHealth)
	}
}

// TestList_MissingAgentTarget_SurfacesUnhealthyEntry verifies a skill whose
// agent target is missing still appears in AgentHealth as unhealthy, rather
// than being dropped from the row (spec 013 Edge Cases §1).
func TestList_MissingAgentTarget_SurfacesUnhealthyEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	// No agent target created — the claude target is missing.

	lf := lockWith("demo", hash)
	if err := saveLock(p.lockPath, lf); err != nil {
		t.Fatalf("saveLock: %v", err)
	}

	got, err := a.List(context.Background(), root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1", len(got))
	}
	want := []AgentHealthEntry{{ID: "claude", Mode: "symlink", Health: string(TargetMissing)}}
	if len(got[0].AgentHealth) != len(want) || got[0].AgentHealth[0] != want[0] {
		t.Errorf("AgentHealth = %+v, want %+v (skill must not be dropped)", got[0].AgentHealth, want)
	}
}

// TestList_AgentHealthOrderMatchesAgentsOrder verifies AgentHealth is built
// in the same order as the existing Agents field (insertion order, as
// recorded in the lock) rather than the alphabetical order sortedKeys would
// produce. A skill whose agents were installed "codex" then "claude" (not
// alphabetical) must keep that same order in both fields, so a consumer
// zipping Agents[i] with AgentHealth[i] never mispairs an agent with another
// agent's health.
func TestList_AgentHealthOrderMatchesAgentsOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	linkAgentAt(t, root, "demo", "codex")
	linkAgentAt(t, root, "demo", "claude")

	lf := skillslock.NewState()
	lf.Skills["demo"] = skillslock.Record{
		Resolved: skillslock.Resolved{ContentHash: hash, Commit: "deadbeef"},
		Installation: skillslock.Installation{
			Scope:      "project",
			Agents:     []string{"codex", "claude"}, // insertion order, not alphabetical
			ActivePath: active.Rel("demo"),
			Targets: map[string]string{
				"codex":  filepath.Join(".codex", "skills", "demo"),
				"claude": filepath.Join(".claude", "skills", "demo"),
			},
			Modes: map[string]string{"codex": "symlink", "claude": "symlink"},
		},
	}
	if err := saveLock(p.lockPath, lf); err != nil {
		t.Fatalf("saveLock: %v", err)
	}

	got, err := a.List(context.Background(), root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1", len(got))
	}
	skill := got[0]
	if want := []string{"codex", "claude"}; len(skill.Agents) != len(want) || skill.Agents[0] != want[0] || skill.Agents[1] != want[1] {
		t.Fatalf("Agents = %v, want %v", skill.Agents, want)
	}
	if len(skill.AgentHealth) != 2 {
		t.Fatalf("got %d AgentHealth entries, want 2", len(skill.AgentHealth))
	}
	if skill.AgentHealth[0].ID != "codex" || skill.AgentHealth[1].ID != "claude" {
		t.Errorf("AgentHealth order = [%s, %s], want [codex, claude] to match Agents order",
			skill.AgentHealth[0].ID, skill.AgentHealth[1].ID)
	}
	if skill.Commit != "deadbeef" {
		t.Errorf("Commit = %q, want %q", skill.Commit, "deadbeef")
	}
	if skill.ContentHash != hash {
		t.Errorf("ContentHash = %q, want %q", skill.ContentHash, hash)
	}
}

// linkAgentAt symlinks id's target to the active entry, mirroring linkAgent
// (health_test.go) but for an arbitrary agent id/directory.
func linkAgentAt(t *testing.T, root, name, id string) {
	t.Helper()
	dest := filepath.Join(root, "."+id, "skills", name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	abs, _ := filepath.Abs(active.Path(root, name))
	if err := os.Symlink(abs, dest); err != nil {
		t.Fatalf("symlink agent: %v", err)
	}
}
