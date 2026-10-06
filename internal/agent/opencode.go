package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/metadata"
)

// openCodeMaxName is OpenCode's limit on a skill's frontmatter name.
const openCodeMaxName = 64

// openCode is the OpenCode adapter: a shared-location agent detected by its
// project config, which also warns about skills OpenCode would refuse to load.
type openCode struct{ sharedAgent }

// NewOpenCode returns the OpenCode adapter. It reads project skills from the
// shared store and global skills from ~/.config/opencode/skills, and is
// detected by .opencode/, opencode.json, or opencode.jsonc (spec 027 FR-008,
// FR-009).
func NewOpenCode() Agent {
	return openCode{sharedAgent{
		id:        "opencode",
		name:      "OpenCode",
		globalDir: []string{".config", "opencode", "skills"},
		markers:   []string{".opencode", "opencode.json", "opencode.jsonc"},
	}}
}

// Advise reports OpenCode's naming rules the skill breaks: OpenCode requires a
// frontmatter name that matches the skill's directory and is at most 64
// characters, and silently skips any skill that does not comply. A SKILL.md
// that cannot be read or parsed yields no advice; install validation owns that.
func (openCode) Advise(skillName, skillDir string) []string {
	content, err := os.ReadFile(filepath.Join(skillDir, integrity.SkillFileName)) //nolint:gosec // installed skill dir
	if err != nil {
		return nil
	}
	doc, err := metadata.ParseLenient(content)
	if err != nil {
		return nil
	}
	name := doc.Frontmatter.Name
	switch {
	case name == "":
		return []string{fmt.Sprintf("opencode: skill %q has no 'name' in SKILL.md; OpenCode will not load it", skillName)}
	case name != skillName:
		return []string{fmt.Sprintf("opencode: skill %q declares name %q; OpenCode requires it to match the directory", skillName, name)}
	case len(name) > openCodeMaxName:
		return []string{fmt.Sprintf("opencode: skill %q name exceeds %d characters; OpenCode will not load it", skillName, openCodeMaxName)}
	}
	return nil
}
