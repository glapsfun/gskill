package installer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
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
	return installer.New(nil, cache.New(filepath.Join(root, "cache")))
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

	// Repo-owned chain (spec 022): the active entry is a real committed
	// directory, and the agent target is a *relative* symlink into it.
	if got := res.ActivePath; got != ".agents/skills/demo" {
		t.Errorf("active path = %q, want .agents/skills/demo", got)
	}
	activeDir := filepath.Join(projectRoot, ".agents", "skills", "demo")
	activeInfo, err := os.Lstat(activeDir)
	if err != nil {
		t.Fatalf("lstat active entry: %v", err)
	}
	if activeInfo.Mode()&os.ModeSymlink != 0 || !activeInfo.IsDir() {
		t.Errorf("active entry is not a real directory: mode %v", activeInfo.Mode())
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

// addScript gives a local skill an executable helper script, the case whose
// content hash used to depend on the owner execute bit (spec 028).
func addScript(t *testing.T, skillDir string) {
	t.Helper()
	path := filepath.Join(skillDir, "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // the test needs an executable script
		t.Fatal(err)
	}
}

// chmodFiles sets mode on every regular file under dir, the way a filesystem
// without POSIX permissions (WSL DrvFs) reports them.
func chmodFiles(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		return os.Chmod(p, mode)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// TestInstall_CommittedHitSkipsExecBitWarnings (spec 028 FR-011): content
// taken from its source warns about executable files, but a restore served
// from the committed copy does not repeat it, so a permission-less checkout
// that reads every file back as executable stays quiet.
func TestInstall_CommittedHitSkipsExecBitWarnings(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	addScript(t, material)
	projectRoot := t.TempDir()
	inst := newInstaller(t)

	req := localRequest(t, projectRoot, material, "demo")
	first, err := inst.Install(context.Background(), req)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if !hasWarning(first.Warnings, "has the executable bit set") {
		t.Fatalf("first install warnings = %q, want the executable-bit warning", first.Warnings)
	}

	chmodFiles(t, filepath.Join(projectRoot, ".agents", "skills", "demo"), 0o755)

	req.ExpectContentHash = first.ContentHash
	again, err := inst.Install(context.Background(), req)
	if err != nil {
		t.Fatalf("restore from the committed copy: %v", err)
	}
	if again.StoreScope != installer.ScopeLabelCommitted {
		t.Errorf("StoreScope = %q, want a committed-content hit", again.StoreScope)
	}
	if hasWarning(again.Warnings, "executable bit") {
		t.Errorf("committed restore repeated executable-bit warnings: %q", again.Warnings)
	}
}

// legacyHashes hashes an installed active entry and fails unless its pre-028
// value differs from the current one, so a test cannot pass vacuously.
func legacyHashes(t *testing.T, projectRoot, name string) integrity.Hashes {
	t.Helper()
	h, err := integrity.HashDir(active.Path(projectRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	if h.LegacyContentHash == h.ContentHash {
		t.Fatalf("%s: legacy and current hashes are equal; the fixture has no executable file to exercise", name)
	}
	return h
}

// TestInstallFromCommitted_LegacyExpectReportsNewHash (spec 028 R3): a lock
// entry recorded by a pre-028 release is accepted against the committed copy,
// and the result reports the current hash so the lock can be upgraded.
func TestInstallFromCommitted_LegacyExpectReportsNewHash(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	addScript(t, material)
	projectRoot := t.TempDir()
	inst := newInstaller(t)

	req := localRequest(t, projectRoot, material, "demo")
	if _, err := inst.Install(context.Background(), req); err != nil {
		t.Fatalf("first install: %v", err)
	}
	h := legacyHashes(t, projectRoot, "demo")

	req.ExpectContentHash = h.LegacyContentHash
	res, err := inst.Install(context.Background(), req)
	if err != nil {
		t.Fatalf("install against a pre-028 hash: %v", err)
	}
	if res.StoreScope != installer.ScopeLabelCommitted {
		t.Errorf("StoreScope = %q, want a committed-content hit", res.StoreScope)
	}
	if res.ContentHash != h.ContentHash {
		t.Errorf("ContentHash = %s, want the current hash %s", res.ContentHash, h.ContentHash)
	}
	if res.LegacyContentHash != h.LegacyContentHash {
		t.Errorf("LegacyContentHash = %s, want %s", res.LegacyContentHash, h.LegacyContentHash)
	}
}

// TestInstall_PreservesExecBitAndWarns (spec 028 FR-011, FR-012): content
// taken from its source keeps its executable bit where the filesystem stores
// it, and the warning is still shown.
func TestInstall_PreservesExecBitAndWarns(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	addScript(t, material)
	projectRoot := t.TempDir()

	res, err := newInstaller(t).Install(context.Background(), localRequest(t, projectRoot, material, "demo"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	info, err := os.Stat(filepath.Join(active.Path(projectRoot, "demo"), "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o100 == 0 {
		t.Errorf("installed script mode = %v, want the executable bit preserved", info.Mode())
	}
	if !hasWarning(res.Warnings, "has the executable bit set") {
		t.Errorf("warnings = %q, want the executable-bit warning", res.Warnings)
	}
}
