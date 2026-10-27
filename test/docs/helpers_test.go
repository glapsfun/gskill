// Package docs holds repository-level checks for the documentation site: they
// read docs/, README.md, examples/, and website/ as committed, so they run under
// `go test ./...` and therefore under scripts/verify.sh.
package docs

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot is the absolute repository root, two levels above this package.
var repoRoot = func() string {
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	return abs
}()

// repoPath joins slash-separated parts onto the repository root.
func repoPath(parts ...string) string {
	return filepath.Join(append([]string{repoRoot}, parts...)...)
}

// readRepoFile returns a repository file's contents, failing the test if it is
// missing or unreadable.
func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(repoPath(parts...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(data)
}
