package docsite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validNav is a manifest that satisfies every structural rule. Tests derive
// broken variants from it with strings.Replace.
const validNav = `base: /gskill
exclude:
  - path: design/**
    reason: internal design records
groups:
  - group: Start here
    pages:
      - path: README.md
        label: Overview
  - group: How-to guides
    pages:
      - how-to/a.md
  - group: Reference
    pages:
      - reference/r.md
  - group: Explanation
    pages:
      - explanation/e.md
  - group: Contributing
    pages:
      - contributing/c.md
    links:
      - label: Releases
        url: https://github.com/glapsfun/gskill/releases
`

// baseTree is a repository fixture matching validNav: every listed page exists
// and the excluded glob matches one file.
func baseTree() map[string]string {
	return map[string]string{
		"docs/nav.yaml":          validNav,
		"docs/README.md":         "# GSKILL docs\n\nOverview of the docs.\n",
		"docs/how-to/a.md":       "# Do A\n\nHow to do A. See [the reference](../reference/r.md#flags).\n",
		"docs/reference/r.md":    "# Reference R\n\nEvery flag.\n\n## Flags\n\nText.\n",
		"docs/explanation/e.md":  "# Explain E\n\nWhy E works.\n",
		"docs/contributing/c.md": "# Contribute\n\nHow to contribute. Read the [license](../../LICENSE).\n",
		"docs/design/x.md":       "# Design X\n\nInternal notes.\n",
		"LICENSE":                "MIT\n",
	}
}

// writeTree materializes files (slash paths relative to root) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// newTree writes files into a fresh temp directory and returns its path.
func newTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, files)
	return root
}

// joinErrs flattens errors into one newline-separated string for substring
// assertions.
func joinErrs(errs []error) string {
	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "\n")
}
