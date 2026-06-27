package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// GC removes store entries whose content hash is not in referenced and returns
// the number removed. The referenced set holds full content keys (e.g.
// "sha256:abcd"). GC only runs on explicit reconciliation (remove, sync
// --prune), never during an additive install (FR-019).
func (s *Store) GC(referenced map[string]bool) (int, error) {
	algos, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read store root: %w", err)
	}

	removed := 0
	for _, algo := range algos {
		if !algo.IsDir() {
			continue
		}
		algoDir := filepath.Join(s.root, algo.Name())
		entries, err := os.ReadDir(algoDir)
		if err != nil {
			return removed, fmt.Errorf("read store %s: %w", algo.Name(), err)
		}
		for _, entry := range entries {
			key := algo.Name() + ":" + entry.Name()
			if referenced[key] {
				continue
			}
			if err := os.RemoveAll(filepath.Join(algoDir, entry.Name())); err != nil {
				return removed, fmt.Errorf("remove store entry %s: %w", key, err)
			}
			removed++
		}
	}
	return removed, nil
}
