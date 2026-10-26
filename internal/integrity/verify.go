package integrity

import (
	"fmt"
	"path/filepath"
)

// VerifyDir resolves any symlink at dir and recomputes the canonical content
// hashes of the installed skill, reporting whether expected is its current or
// its pre-028 identity (Hashes.Matches). It fails closed: any error (including
// a missing target) is returned to the caller rather than treated as a pass
// (FR-015).
func VerifyDir(dir, expected string) (ok bool, actual Hashes, err error) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, Hashes{}, fmt.Errorf("resolve %s: %w", dir, err)
	}
	h, err := HashDir(resolved)
	if err != nil {
		return false, Hashes{}, err
	}
	return h.Matches(expected), h, nil
}
