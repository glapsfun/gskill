package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/integrity"
)

// dirAgent is a convention-based adapter for agents that keep skills under
// <base>/<markerDir>/skills/<name> and are detected by the presence of
// <markerDir>. Agents that diverge from this layout get their own type.
type dirAgent struct {
	id        string
	name      string
	markerDir string
	symlinks  bool
}

// ID returns the stable identifier.
func (a dirAgent) ID() string { return a.id }

// DisplayName returns the human-facing name.
func (a dirAgent) DisplayName() string { return a.name }

// ProjectSkillDir returns the per-project skills container directory.
func (a dirAgent) ProjectSkillDir(projectRoot string) string {
	return filepath.Join(projectRoot, a.markerDir, "skills")
}

// GlobalSkillDir returns the user-global skills container directory.
func (a dirAgent) GlobalSkillDir(home string) string {
	return filepath.Join(home, a.markerDir, "skills")
}

// SupportsSymlinks reports whether the agent tolerates symlinked skills.
func (a dirAgent) SupportsSymlinks() bool { return a.symlinks }

// Detect reports whether the agent's marker directory exists under projectRoot.
func (a dirAgent) Detect(_ context.Context, projectRoot string) (bool, error) {
	info, err := os.Stat(filepath.Join(projectRoot, a.markerDir))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("detect %s: %w", a.id, err)
	}
	return info.IsDir(), nil
}

// ValidateInstallation checks that a SKILL.md is present in skillDir.
func (a dirAgent) ValidateInstallation(_ context.Context, skillDir string) error {
	if _, err := os.Stat(filepath.Join(skillDir, integrity.SkillFileName)); err != nil {
		return fmt.Errorf("agent %s: %s missing in %s: %w", a.id, integrity.SkillFileName, skillDir, err)
	}
	return nil
}
