package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// TestExitCodes_NoSkillFoundIsExit5 covers the discovery exit-code contract:
// a source with no SKILL.md anywhere is source-unavailable (exit 5).
func TestExitCodes_NoSkillFoundIsExit5(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "README.md"), []byte("# not a skill\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proj := newProject(t)
	mustInit(t, proj)

	_, _, code := runGskill(t, proj, "add", empty)
	if code != 5 {
		t.Errorf("exit = %d, want 5 (no SKILL.md found)", code)
	}
}

// TestAdd_DefaultsToClaude covers the default-agent behavior: with no --agent and
// no detected agent, an install targets Claude (.claude/skills).
func TestAdd_DefaultsToClaude(t *testing.T) {
	t.Parallel()

	src := localTreeSkill(t, "skills/demo", "demo")
	proj := t.TempDir() // no agent markers at all
	mustInit(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", src); code != 0 {
		t.Fatalf("add with no agent should default to claude, exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("skill not installed into the default .claude agent dir: %v", err)
	}
}

// TestExitCodes_NoAgentIsExit9 covers exit 9: a valid skill but no target agent
// available at all (a registry without the default and none detected).
func TestExitCodes_NoAgentIsExit9(t *testing.T) {
	t.Parallel()

	src := localTreeSkill(t, "skills/demo", "demo")
	proj := t.TempDir() // no agent markers
	reg := agent.NewRegistry()
	if err := reg.Register(agent.NewCodex()); err != nil {
		t.Fatal(err)
	}
	a := app.New(app.Options{Agents: reg, Logger: discardLogger()})

	if _, stderr, code := runGskillWithApp(t, a, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	_, _, code := runGskillWithApp(t, a, proj, "add", src)
	if code != 9 {
		t.Errorf("exit = %d, want 9 (no target agent and no default available)", code)
	}
}
