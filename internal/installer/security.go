package installer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
)

// validateContent scans a skill directory before staging. It rejects symlinks
// that escape the skill directory (path-traversal / unsafe symlinks, FR-042)
// and returns warnings for executable-bit files, which gskill never runs
// (FR-043). Content is never executed.
func validateContent(dir string) ([]string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve skill dir: %w", err)
	}

	var warnings []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return checkSymlink(root, p)
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&0o111 != 0 {
			rel, _ := filepath.Rel(root, p)
			warnings = append(warnings, fmt.Sprintf("file %q has the executable bit set; gskill never executes skill content", rel))
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return warnings, nil
}

// checkSymlink rejects a symlink whose target escapes root (FR-042).
func checkSymlink(root, p string) error {
	target, err := os.Readlink(p)
	if err != nil {
		return fmt.Errorf("read link %s: %w", p, err)
	}

	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(p), target)
	}
	resolved = filepath.Clean(resolved)

	rel, err := filepath.Rel(root, resolved)
	if err != nil || filepath.IsAbs(target) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: skill contains an unsafe symlink %q -> %q that escapes the skill directory",
			errs.ErrIntegrity, p, target)
	}
	return nil
}
