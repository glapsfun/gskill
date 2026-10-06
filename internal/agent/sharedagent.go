package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/integrity"
)

// sharedAgent is the adapter for agents that read project skills straight from
// gskill's repo-owned store (.agents/skills), so gskill places nothing per
// agent inside the project (spec 027). Their user-global dir is unrelated to
// any project marker, and markers may be files or directories; an agent with
// no markers is never auto-detected.
type sharedAgent struct {
	id        string
	name      string
	globalDir []string
	markers   []string
}

// ID returns the stable identifier.
func (a sharedAgent) ID() string { return a.id }

// DisplayName returns the human-facing name.
func (a sharedAgent) DisplayName() string { return a.name }

// ProjectSkillDir returns the repo-owned store, which the agent reads directly.
func (a sharedAgent) ProjectSkillDir(projectRoot string) string {
	return active.Dir(projectRoot)
}

// GlobalSkillDir returns the agent's user-global skills container directory.
func (a sharedAgent) GlobalSkillDir(home string) string {
	return filepath.Join(append([]string{home}, a.globalDir...)...)
}

// SupportsSymlinks reports whether the agent tolerates symlinked skills.
func (a sharedAgent) SupportsSymlinks() bool { return true }

// Detect reports whether any of the agent's project markers exists.
func (a sharedAgent) Detect(_ context.Context, projectRoot string) (bool, error) {
	for _, m := range a.markers {
		if _, err := os.Stat(filepath.Join(projectRoot, m)); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("detect %s: %w", a.id, err)
		}
	}
	return false, nil
}

// ValidateInstallation checks that a SKILL.md is present in skillDir.
func (a sharedAgent) ValidateInstallation(_ context.Context, skillDir string) error {
	if _, err := os.Stat(filepath.Join(skillDir, integrity.SkillFileName)); err != nil {
		return fmt.Errorf("agent %s: %s missing in %s: %w", a.id, integrity.SkillFileName, skillDir, err)
	}
	return nil
}
