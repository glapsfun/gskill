package store

import (
	"os"

	"github.com/glapsfun/gskill/internal/fsutil"
)

// Store is the content-addressed store of installed skill content, keyed by
// canonical content hash. It is the single source of truth that agent
// directories link to or copy from (FR-019).
type Store struct {
	root string
}

// New returns a Store rooted at root.
func New(root string) *Store {
	return &Store{root: root}
}

// Root returns the store's root directory.
func (s *Store) Root() string { return s.root }

// Path returns the directory for a content hash, whether or not it exists.
func (s *Store) Path(contentHash string) string {
	return fsutil.KeyPath(s.root, contentHash)
}

// Has reports whether content for contentHash is present.
func (s *Store) Has(contentHash string) bool {
	_, err := os.Stat(s.Path(contentHash))
	return err == nil
}

// Put imports srcDir under contentHash and returns the stored path. It is
// idempotent.
func (s *Store) Put(contentHash, srcDir string) (string, error) {
	return fsutil.ImportDir(s.root, contentHash, srcDir)
}
