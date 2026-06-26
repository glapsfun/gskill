package installer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// stagingPrefixes are the temp-dir name prefixes used during a transaction.
// Leftovers indicate a crash mid-install and are safe to remove (FR-024).
var stagingPrefixes = []string{".import-", ".fetch-"}

// CleanupStaging removes orphaned staging temp directories left under the given
// roots by an interrupted install, so a crash never leaves torn state behind
// (FR-024, SC-007). It returns the number of entries removed.
func CleanupStaging(roots ...string) (int, error) {
	removed := 0
	for _, root := range roots {
		n, err := cleanupRoot(root)
		if err != nil {
			return removed, err
		}
		removed += n
	}
	return removed, nil
}

// cleanupRoot removes staging temp dirs directly under root and its algorithm
// subdirectories (where ImportDir stages content).
func cleanupRoot(root string) (int, error) {
	removed := 0
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if !d.IsDir() || p == root {
			return nil
		}
		if isStaging(d.Name()) {
			if rmErr := os.RemoveAll(p); rmErr != nil {
				return fmt.Errorf("remove staging %s: %w", p, rmErr)
			}
			removed++
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return removed, walkErr
	}
	return removed, nil
}

// isStaging reports whether name is a staging temp directory.
func isStaging(name string) bool {
	for _, prefix := range stagingPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
