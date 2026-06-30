package integration_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// acmeAgent is a throwaway adapter implementing agent.Agent for a fictional
// "acme" agent, proving a new agent needs only a new adapter (FR-026, SC-007).
type acmeAgent struct{}

func (acmeAgent) ID() string          { return "acme" }
func (acmeAgent) DisplayName() string { return "Acme" }

func (acmeAgent) Detect(context.Context, string) (bool, error) { return false, nil }

func (acmeAgent) ProjectSkillDir(root string) string {
	return filepath.Join(root, ".acme", "skills")
}

func (acmeAgent) GlobalSkillDir(home string) string {
	return filepath.Join(home, ".acme", "skills")
}
func (acmeAgent) SupportsSymlinks() bool { return true }

func (acmeAgent) ValidateInstallation(_ context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return err
	}
	return nil
}

// appWithAcme builds an App whose registry adds the acme adapter alongside the
// built-in agents, with no change to the installer.
func appWithAcme() *app.App {
	reg := agent.NewRegistry()
	_ = reg.Register(agent.NewClaudeCode())
	_ = reg.Register(acmeAgent{})
	return app.New(app.Options{
		Agents: reg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// TestAdapterExtensibility_NewAgentEndToEnd drives the full lifecycle for a
// brand-new agent adapter through the unchanged installer and reconcile code.
func TestAdapterExtensibility_NewAgentEndToEnd(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	a := appWithAcme()

	if _, stderr, code := runGskillWithApp(t, a, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	// add → the new agent gets a target through the shared active layer.
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo, "--version", "^1.0.0", "--agent", "acme"); code != 0 {
		t.Fatalf("add acme: %s", stderr)
	}
	requireResolvesActive(t, proj, ".acme", "demo")

	// sync (idempotent), check, status all work for the new agent.
	if _, stderr, code := runGskillWithApp(t, a, proj, "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}
	if _, stderr, code := runGskillWithApp(t, a, proj, "check", "--fail-on-drift"); code != 0 {
		t.Fatalf("check: %s", stderr)
	}

	// repair heals a broken acme target.
	if err := os.RemoveAll(filepath.Join(proj, ".acme", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskillWithApp(t, a, proj, "repair"); code != 0 {
		t.Fatalf("repair: %s", stderr)
	}
	requireResolvesActive(t, proj, ".acme", "demo")

	// unlink --prune removes the last agent and the skill.
	if _, stderr, code := runGskillWithApp(t, a, proj, "unlink", "demo", "--agent", "acme", "--prune"); code != 0 {
		t.Fatalf("unlink: %s", stderr)
	}
	if n := countActiveEntries(t, proj); n != 0 {
		t.Errorf("active entry not pruned for new agent (count=%d)", n)
	}
}

// TestCopyFallback_SharedContentIsReal covers SC-009: in copy mode a shared
// install gives each agent real, readable content (not a dangling link).
func TestCopyFallback_SharedContentIsReal(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude", "--copy"); code != 0 {
		t.Fatalf("add claude --copy: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "codex", "--copy"); code != 0 {
		t.Fatalf("add codex --copy: %s", stderr)
	}
	for _, marker := range []string{".claude", ".codex"} {
		target := filepath.Join(proj, marker, "skills", "demo")
		info, err := os.Lstat(target)
		if err != nil {
			t.Fatalf("lstat %s: %v", target, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s target is a symlink in copy mode", marker)
		}
		if _, err := os.ReadFile(filepath.Join(target, "SKILL.md")); err != nil { //nolint:gosec // test reads its own temp dir
			t.Errorf("%s copy missing real content: %v", marker, err)
		}
	}
}
