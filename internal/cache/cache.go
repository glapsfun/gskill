package cache

import (
	"os"

	"github.com/glapsfun/gskill/internal/fsutil"
)

// Cache is the content-addressed cache of fetched source material, keyed by an
// immutable fetch key (typically a commit SHA). A warm cache enables offline
// reproduction (FR-025, FR-026).
type Cache struct {
	root string
}

// New returns a Cache rooted at root.
func New(root string) *Cache {
	return &Cache{root: root}
}

// Root returns the cache's root directory.
func (c *Cache) Root() string { return c.root }

// Path returns the directory for a fetch key, whether or not it exists.
func (c *Cache) Path(key string) string {
	return fsutil.KeyPath(c.root, key)
}

// Has reports whether material for key is cached.
func (c *Cache) Has(key string) bool {
	_, err := os.Stat(c.Path(key))
	return err == nil
}

// Put imports fetched material from srcDir under key and returns the cached
// path. It is idempotent, so a warm entry is reused rather than refetched.
func (c *Cache) Put(key, srcDir string) (string, error) {
	return fsutil.ImportDir(c.root, key, srcDir)
}
