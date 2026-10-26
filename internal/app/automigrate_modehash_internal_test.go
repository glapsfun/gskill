package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/integrity"
)

// TestSeedFromLegacyStore_LegacyHashActivatesWithNewHash (spec 028 R3): a
// pre-022 store object recorded under a pre-028 hash still seeds the active
// entry, and the entry verifies against the current hash.
func TestSeedFromLegacyStore_LegacyHashActivatesWithNewHash(t *testing.T) {
	t.Parallel()

	content := filepath.Join(t.TempDir(), "content")
	files := map[string]string{"SKILL.md": "# demo\n", "scripts/run.sh": "#!/bin/sh\necho ok\n"}
	for rel, body := range files {
		full := filepath.Join(content, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(content, "scripts", "run.sh"), 0o755); err != nil { //nolint:gosec // the fixture needs an executable script
		t.Fatal(err)
	}
	h, err := integrity.HashDir(content)
	if err != nil {
		t.Fatal(err)
	}
	if h.LegacyContentHash == h.ContentHash {
		t.Fatal("legacy and current hashes are equal; the fixture has no executable file to exercise")
	}

	root := t.TempDir()
	hex := strings.TrimPrefix(h.LegacyContentHash, "sha256:")
	obj := filepath.Join(root, stateDirName, "store", "sha256", hex, "content")
	if err := os.MkdirAll(filepath.Dir(obj), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(content, obj); err != nil {
		t.Fatal(err)
	}

	(&App{}).seedFromLegacyStore(openProject(root), "demo", h.LegacyContentHash)

	if got, _ := active.HealthOf(root, "demo", h.ContentHash); got != active.HealthOK {
		t.Errorf("active entry health against the current hash = %q, want ok", got)
	}
}
