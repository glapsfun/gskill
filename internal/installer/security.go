package installer

import (
	"github.com/glapsfun/gskill/internal/integrity"
)

// validateContent scans a skill directory before staging. It rejects symlinks
// that escape the skill directory (path-traversal / unsafe symlinks, FR-042)
// and returns warnings for executable-bit files, which gskill never runs
// (FR-043). Content is never executed. The shared implementation lives in
// integrity.ValidateContent so store admission applies identical checks.
func validateContent(dir string) ([]string, error) {
	return integrity.ValidateContent(dir)
}
