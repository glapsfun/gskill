package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/cache"
)

func TestCache_PutReuseAndOfflineHit(t *testing.T) {
	t.Parallel()

	material := t.TempDir()
	if err := os.WriteFile(filepath.Join(material, "tree.txt"), []byte("commit-tree"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := cache.New(t.TempDir())
	const commit = "6c58cfd49a71d86d7d225c61ea63d98c3df19bd1"

	if c.Has(commit) {
		t.Fatal("cache hit before Put")
	}
	path, err := c.Put(commit, material)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !c.Has(commit) {
		t.Error("cache miss after Put (offline reuse would fail)")
	}
	if c.Path(commit) != path {
		t.Errorf("Path = %q, want %q", c.Path(commit), path)
	}
	if got, err := os.ReadFile(filepath.Join(path, "tree.txt")); err != nil || string(got) != "commit-tree" { //nolint:gosec // test path
		t.Errorf("cached content = %q, err=%v", got, err)
	}
}
