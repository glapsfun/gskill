package installer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/store"
)

// localSkill creates a local skill directory and returns it.
func localSkill(t *testing.T, name string) string {
	t.Helper()

	dir := t.TempDir()
	body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newInstaller(t *testing.T) *installer.Installer {
	t.Helper()
	root := t.TempDir()
	return installer.New(nil, cache.New(filepath.Join(root, "cache")), store.New(filepath.Join(root, "store")))
}

func localRequest(t *testing.T, projectRoot, materialDir, name string) installer.Request {
	t.Helper()

	ref, err := source.Parse(materialDir)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	return installer.Request{
		Ref:         ref,
		Revision:    resolver.Revision{RefKind: resolver.RefKindLocal, MutableRef: true},
		Name:        name,
		Agents:      []agent.Agent{agent.NewClaudeCode()},
		Scope:       installer.ScopeProject,
		ModePref:    "symlink",
		ProjectRoot: projectRoot,
	}
}

func TestInstall_StagesAndActivates(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	projectRoot := t.TempDir()
	inst := newInstaller(t)

	res, err := inst.Install(context.Background(), localRequest(t, projectRoot, material, "demo"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if res.ContentHash == "" {
		t.Error("empty content hash")
	}
	// Activated into the Claude Code skills dir.
	dest := filepath.Join(projectRoot, ".claude", "skills", "demo", "SKILL.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("skill not activated at %s: %v", dest, err)
	}
	if got := res.Targets["claude-code"]; got != filepath.Join(".claude", "skills", "demo") {
		t.Errorf("target = %q, want .claude/skills/demo", got)
	}
}

func TestInstall_NameMismatchRejected(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "actual-name")
	inst := newInstaller(t)

	req := localRequest(t, t.TempDir(), material, "declared-name")
	if _, err := inst.Install(context.Background(), req); err == nil {
		t.Error("expected rejection when frontmatter name != declared key (FR-013)")
	}
}

func TestInstall_Idempotent(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	projectRoot := t.TempDir()
	inst := newInstaller(t)
	req := localRequest(t, projectRoot, material, "demo")

	first, err := inst.Install(context.Background(), req)
	if err != nil {
		t.Fatalf("first Install: %v", err)
	}
	second, err := inst.Install(context.Background(), req)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if first.ContentHash != second.ContentHash {
		t.Errorf("content hash changed on re-install: %s vs %s", first.ContentHash, second.ContentHash)
	}
}
