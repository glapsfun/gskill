package docs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/docs"
)

// repoPath maps a repo-relative path to one relative to this test's package
// directory (internal/docs), so the test can read the committed reference files.
func repoPath(rel string) string {
	return filepath.Join("..", "..", rel)
}

// TestReferenceGolden fails if the committed reference Markdown drifts from what
// the generator produces from the live CLI grammar / exit-code table. Fix by
// running `go run ./cmd/gen-reference` and committing the result.
func TestReferenceGolden(t *testing.T) {
	t.Parallel()

	files, err := docs.RenderAll()
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("RenderAll returned no files")
	}

	for rel, want := range files {
		got, err := os.ReadFile(repoPath(rel))
		if err != nil {
			t.Fatalf("read committed %s: %v (run: go run ./cmd/gen-reference)", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s is stale; regenerate with `go run ./cmd/gen-reference` and commit.\n"+
				"committed %d bytes, generated %d bytes", rel, len(got), len(want))
		}
	}
}

// TestRenderIsDeterministic guards Principle I: the same grammar must render
// byte-identically on every run (no map-order or timestamp leakage).
func TestRenderIsDeterministic(t *testing.T) {
	t.Parallel()

	first, err := docs.RenderAll()
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	second, err := docs.RenderAll()
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for path, a := range first {
		if b := second[path]; a != b {
			t.Errorf("%s render not deterministic across runs", path)
		}
	}
}

// TestExitCodesIncludeKnownContract is a lightweight content check that the
// generated exit-code reference documents the codes scripts depend on.
func TestExitCodesIncludeKnownContract(t *testing.T) {
	t.Parallel()

	out := docs.RenderExitCodes()
	for _, want := range []string{
		"| 0 | success |",
		"`--frozen-lockfile`",
		"integrity failure",
		"| 12 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exit-codes reference missing %q", want)
		}
	}
}
