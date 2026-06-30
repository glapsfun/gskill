package installer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/discovery"
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
	if got := res.Targets["claude"]; got != filepath.Join(".claude", "skills", "demo") {
		t.Errorf("target = %q, want .claude/skills/demo", got)
	}

	// Three-hop chain: the active entry exists and links into the store, and the
	// agent target is a symlink into the active entry (not directly into store).
	if got := res.ActivePath; got != filepath.Join(".agents", "skills", "demo") {
		t.Errorf("active path = %q, want .agents/skills/demo", got)
	}
	activeDir := filepath.Join(projectRoot, ".agents", "skills", "demo")
	activeInfo, err := os.Lstat(activeDir)
	if err != nil {
		t.Fatalf("lstat active entry: %v", err)
	}
	if activeInfo.Mode()&os.ModeSymlink == 0 {
		t.Error("active entry is not a symlink into the store")
	} else if tgt, _ := os.Readlink(activeDir); !strings.Contains(tgt, string(filepath.Separator)+"store"+string(filepath.Separator)) {
		t.Errorf("active entry links to %q, want a store path", tgt)
	}

	agentTarget := filepath.Join(projectRoot, ".claude", "skills", "demo")
	agentInfo, err := os.Lstat(agentTarget)
	if err != nil {
		t.Fatalf("lstat agent target: %v", err)
	}
	if agentInfo.Mode()&os.ModeSymlink == 0 {
		t.Error("agent target is not a symlink")
	} else {
		tgt, _ := os.Readlink(agentTarget)
		if !strings.Contains(tgt, filepath.Join(".agents", "skills", "demo")) {
			t.Errorf("agent target links to %q, want the active entry .agents/skills/demo", tgt)
		}
	}
}

// nestedSkill creates a source with one skill at skills/<folder> whose
// frontmatter name may differ from the folder, and returns the source root.
func nestedSkill(t *testing.T, folder, frontmatterName string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "skills", folder)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + frontmatterName + "\ndescription: a skill\n---\n# " + folder + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInstall_FolderIdentityNotFrontmatterName(t *testing.T) {
	t.Parallel()

	// Folder "foo" but frontmatter name "bar": identity is the folder; the
	// install proceeds (keyed by req.Name == folder id) and warns about the
	// mismatch rather than failing (research R3, FR-007/FR-008).
	material := nestedSkill(t, "foo", "bar")
	projectRoot := t.TempDir()
	inst := newInstaller(t)

	req := localRequest(t, projectRoot, material, "foo")
	req.Path = "skills/foo"
	res, err := inst.Install(context.Background(), req)
	if err != nil {
		t.Fatalf("Install: %v (folder-identity mismatch must not fail)", err)
	}
	var warned bool
	for _, w := range res.Warnings {
		if strings.Contains(w, "bar") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a frontmatter/folder mismatch warning, got %v", res.Warnings)
	}
	dest := filepath.Join(projectRoot, ".claude", "skills", "foo", "SKILL.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("skill not activated: %v", err)
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

// multiSkillSource creates a local source tree with several skills.
func multiSkillSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDiscoverAll_LocalSourceReadOnly(t *testing.T) {
	t.Parallel()

	material := multiSkillSource(t)
	projectRoot := t.TempDir()
	inst := newInstaller(t)
	req := localRequest(t, projectRoot, material, "")

	res, err := inst.DiscoverAll(context.Background(), req, discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(res.Skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(res.Skills))
	}
	// Read-only: nothing activated into the project.
	if _, err := os.Stat(filepath.Join(projectRoot, ".claude")); err == nil {
		t.Error("DiscoverAll must not activate skills into the project")
	}
}
