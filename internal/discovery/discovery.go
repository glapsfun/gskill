package discovery

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/metadata"
)

// Sentinel discovery errors.
var (
	// ErrNoSkill is returned when no SKILL.md can be found.
	ErrNoSkill = errors.New("no SKILL.md found")
	// ErrAmbiguousSkill is returned when multiple skills are found and no
	// explicit path selects one.
	ErrAmbiguousSkill = errors.New("multiple SKILL.md found; specify a path")
)

// Skill is a discovered, validated skill bundle.
type Skill struct {
	// Dir is the absolute directory containing SKILL.md.
	Dir string
	// RelDir is Dir relative to the search root (forward-slash, "." at root).
	RelDir string
	// Frontmatter is the parsed, validated SKILL.md frontmatter.
	Frontmatter metadata.Frontmatter
	// Body is the markdown body.
	Body []byte
	// Warnings carries non-fatal validation warnings (e.g. unknown keys).
	Warnings []string
}

// Discover locates the skill within root. When explicitPath is set, only that
// subpath is consulted (explicit path wins, FR-011); otherwise the tree is
// searched and exactly one SKILL.md must be present (FR-011, FR-013).
func Discover(root, explicitPath string) (Skill, error) {
	if explicitPath != "" {
		return loadSkill(root, filepath.Join(root, explicitPath))
	}

	dirs, err := findSkillDirs(root)
	if err != nil {
		return Skill{}, err
	}
	switch len(dirs) {
	case 0:
		return Skill{}, fmt.Errorf("%w under %s", ErrNoSkill, root)
	case 1:
		return loadSkill(root, dirs[0])
	default:
		return Skill{}, fmt.Errorf("%w: found %d in %s", ErrAmbiguousSkill, len(dirs), root)
	}
}

// findSkillDirs returns the directories under root that contain a SKILL.md.
func findSkillDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == integrity.SkillFileName {
			dirs = append(dirs, filepath.Dir(p))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search for skills under %s: %w", root, err)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// loadSkill reads and validates the SKILL.md in dir.
func loadSkill(root, dir string) (Skill, error) {
	path := filepath.Join(dir, integrity.SkillFileName)
	content, err := os.ReadFile(path) //nolint:gosec // discovering within a caller-provided root
	if err != nil {
		if os.IsNotExist(err) {
			return Skill{}, fmt.Errorf("%w at %s", ErrNoSkill, dir)
		}
		return Skill{}, fmt.Errorf("read %s: %w", path, err)
	}

	doc, err := metadata.Parse(content)
	if err != nil {
		return Skill{}, fmt.Errorf("validate %s: %w", path, err)
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return Skill{}, fmt.Errorf("relativize %s: %w", dir, err)
	}

	return Skill{
		Dir:         dir,
		RelDir:      filepath.ToSlash(rel),
		Frontmatter: doc.Frontmatter,
		Body:        doc.Body,
		Warnings:    doc.Warnings,
	}, nil
}
