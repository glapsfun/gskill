package installer_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/installer"
)

// sharedAgent is a test-only shared-location agent: its project skill dir is
// the repo-owned store itself, like Antigravity CLI or OpenCode.
type sharedAgent struct{}

func (sharedAgent) ID() string                                   { return "sharedfake" }
func (sharedAgent) DisplayName() string                          { return "Shared Fake" }
func (sharedAgent) Detect(context.Context, string) (bool, error) { return false, nil }
func (sharedAgent) ProjectSkillDir(root string) string {
	return filepath.Join(root, ".agents", "skills")
}

func (sharedAgent) GlobalSkillDir(home string) string {
	return filepath.Join(home, ".sharedfake", "skills")
}
func (sharedAgent) SupportsSymlinks() bool { return true }

// ValidateInstallation fails unless dir holds a real SKILL.md, so a store
// entry clobbered into a self-referencing link is caught.
func (sharedAgent) ValidateInstallation(_ context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return errors.New("SKILL.md missing in " + dir)
	}
	return nil
}

func requireRealActiveEntry(t *testing.T, projectRoot, name string) {
	t.Helper()
	dir := filepath.Join(projectRoot, ".agents", "skills", name)
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatalf("lstat active entry: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("active entry is not a real directory: mode %v", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("active entry lost its content: %v", err)
	}
}

func TestInstall_SharedTargetIsRecordedNotActivated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pref       string
		claudeMode string
	}{
		{"symlink preference", installer.PrefSymlink, string(installer.ModeSymlink)},
		{"copy preference", installer.PrefCopy, string(installer.ModeCopy)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			material := localSkill(t, "demo")
			projectRoot := t.TempDir()
			req := localRequest(t, projectRoot, material, "demo")
			req.Agents = []agent.Agent{sharedAgent{}, agent.NewClaudeCode()}
			req.ModePref = tc.pref

			res, err := newInstaller(t).Install(context.Background(), req)
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			requireRealActiveEntry(t, projectRoot, "demo")
			if got := res.Targets["sharedfake"]; got != ".agents/skills/demo" {
				t.Errorf("shared target = %q, want .agents/skills/demo", got)
			}
			if got := res.Modes["sharedfake"]; got != string(installer.ModeShared) {
				t.Errorf("shared mode = %q, want %q", got, installer.ModeShared)
			}
			if got := res.Modes["claude"]; got != tc.claudeMode {
				t.Errorf("claude mode = %q, want %q", got, tc.claudeMode)
			}
			if got := string(res.Mode); got != tc.claudeMode {
				t.Errorf("representative mode = %q, want the first non-shared agent's %q", got, tc.claudeMode)
			}
		})
	}
}

func TestInstall_AllSharedTargetsNeverRecordSharedAsRepresentative(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pref string
		want installer.Mode
	}{
		{installer.PrefAuto, installer.ModeSymlink},
		{installer.PrefSymlink, installer.ModeSymlink},
		{installer.PrefCopy, installer.ModeCopy},
	}
	for _, tc := range cases {
		t.Run(tc.pref, func(t *testing.T) {
			t.Parallel()

			material := localSkill(t, "demo")
			projectRoot := t.TempDir()
			req := localRequest(t, projectRoot, material, "demo")
			req.Agents = []agent.Agent{sharedAgent{}}
			req.ModePref = tc.pref

			res, err := newInstaller(t).Install(context.Background(), req)
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			requireRealActiveEntry(t, projectRoot, "demo")
			if got := res.Modes["sharedfake"]; got != string(installer.ModeShared) {
				t.Errorf("shared mode = %q, want %q", got, installer.ModeShared)
			}
			if res.Mode != tc.want {
				t.Errorf("representative mode = %q, want %q (never %q)", res.Mode, tc.want, installer.ModeShared)
			}
		})
	}
}

func TestInstall_SharedTargetSurvivesReinstall(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	projectRoot := t.TempDir()
	inst := newInstaller(t)
	req := localRequest(t, projectRoot, material, "demo")
	req.Agents = []agent.Agent{sharedAgent{}, agent.NewClaudeCode()}

	for i := range 2 {
		if _, err := inst.Install(context.Background(), req); err != nil {
			t.Fatalf("Install #%d: %v", i+1, err)
		}
		requireRealActiveEntry(t, projectRoot, "demo")
	}
}
