package integrity

import (
	"fmt"
	"path/filepath"
)

// VerifyDir resolves any symlink at dir and recomputes the canonical content
// hash of the installed skill, reporting whether it matches expected. It fails
// closed: any error (including a missing target) is returned to the caller
// rather than treated as a pass (FR-015).
func VerifyDir(dir, expected string) (ok bool, actual string, err error) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	h, err := HashDir(resolved)
	if err != nil {
		return false, "", err
	}
	return h.ContentHash == expected, h.ContentHash, nil
}
