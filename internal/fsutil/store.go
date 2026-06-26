package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeyPath maps a content key such as "sha256:abcd" to a directory under root,
// splitting the algorithm prefix into its own subdirectory.
func KeyPath(root, key string) string {
	return filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(key, ":", "/")))
}

// ImportDir atomically copies srcDir into the content-addressed location for key
// under root and returns the final path. It is idempotent: an entry that already
// exists is left untouched.
func ImportDir(root, key, srcDir string) (string, error) {
	dest := KeyPath(root, key)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}

	parent := filepath.Dir(dest)
	tmp, err := TempDir(parent, ".import-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	stage := filepath.Join(tmp, "content")
	if err := CopyDir(srcDir, stage); err != nil {
		return "", err
	}
	if err := os.MkdirAll(parent, dirPerm); err != nil {
		return "", fmt.Errorf("create store parent: %w", err)
	}
	if err := os.Rename(stage, dest); err != nil {
		// A concurrent importer may have won the race; that is fine.
		if _, statErr := os.Stat(dest); statErr == nil {
			return dest, nil
		}
		return "", fmt.Errorf("promote into store: %w", err)
	}
	return dest, nil
}
