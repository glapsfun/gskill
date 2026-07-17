package globalstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/home"
)

// pinPath returns the pin marker path for key.
func (s *Store) pinPath(key string) string {
	return filepath.Join(s.home.PinsDir(), safeKeyName(key))
}

// Pin exempts the object for key from garbage collection (FR-026). Pinning
// an unknown object is an error.
func (s *Store) Pin(key string) error {
	if !s.Has(key) {
		return errs.WithHint(
			fmt.Errorf("%w: store object %s not found", errs.ErrUsage, key),
			"run 'gskill store verify' to list objects")
	}
	return os.WriteFile(s.pinPath(key), nil, home.FilePerm())
}

// Unpin removes the GC exemption for key. Unpinning a never-pinned object is
// a no-op.
func (s *Store) Unpin(key string) error {
	if err := os.Remove(s.pinPath(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unpin %s: %w", key, err)
	}
	return nil
}

// Pinned reports whether key is pinned.
func (s *Store) Pinned(key string) bool {
	_, err := os.Stat(s.pinPath(key))
	return err == nil
}

// Pins lists every pinned content key, sorted.
func (s *Store) Pins() ([]string, error) {
	entries, err := os.ReadDir(s.home.PinsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pins: %w", err)
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if i := strings.Index(name, "-"); i > 0 {
			keys = append(keys, name[:i]+":"+name[i+1:])
		}
	}
	sort.Strings(keys)
	return keys, nil
}
