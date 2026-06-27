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

// run renders the reference documents and writes them to their repo paths.
func run() error {
	files, err := docs.RenderAll()
	if err != nil {
		return err
	}
	for path, content := range files {
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
