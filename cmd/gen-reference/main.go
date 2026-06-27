// Command gen-reference writes GSKILL's generated reference Markdown
// (docs/reference/commands.md and docs/reference/exit-codes.md) from the live
// CLI grammar and exit-code table. Run it from the repo root:
//
//	go run ./cmd/gen-reference
//
// The output is deterministic; commit the result. A golden test
// (internal/docs) fails the build if the committed files drift from the tool.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/docs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-reference:", err)
		os.Exit(1)
	}
}

// run renders the reference documents and writes them under the repo root. It
// resolves the root from the current directory so it works both from the repo
// root (`go run ./cmd/gen-reference`) and via `go generate ./...`, which runs
// with the working directory set to the calling package.
func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	files, err := docs.RenderAll()
	if err != nil {
		return err
	}
	for rel, content := range files {
		relPath := filepath.FromSlash(rel)
		path := filepath.Join(root, relPath)

		relToRoot, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		if relToRoot == ".." || (len(relToRoot) >= 3 && relToRoot[0:3] == ".."+string(os.PathSeparator)) {
			return fmt.Errorf("refusing to write outside repo root: %q", rel)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Println("wrote", path)
	}
	return nil
}

// repoRoot walks up from the working directory until it finds the go.mod that
// marks the module root.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
