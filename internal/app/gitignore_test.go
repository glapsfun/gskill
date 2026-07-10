package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// hasLine reports whether data contains pattern as a whole trimmed line.
func hasLine(data, pattern string) bool {
	for _, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == pattern {
			return true
		}
	}
	return false
}

func TestInit_GitignoresStoreAndActiveLayer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := app.New(app.Options{})

	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, pattern := range []string{".gskill/", ".agents/"} {
		if !hasLine(string(data), pattern) {
			t.Errorf(".gitignore missing %q\n--- content ---\n%s", pattern, data)
		}
	}
}

func TestInit_GitignoreIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := app.New(app.Options{})

	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("Init not idempotent on .gitignore:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	// Exactly one occurrence of each pattern.
	for _, pattern := range []string{".gskill/", ".agents/"} {
		if n := strings.Count(string(second), pattern); n != 1 {
			t.Errorf("pattern %q appears %d times, want 1", pattern, n)
		}
	}
}
