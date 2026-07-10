package integrity_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

// expectedCompat is one recorded reference hash (see testdata/compat/README.md).
type expectedCompat struct {
	ComputedHash string `json:"computedHash"`
	RecordedWith string `json:"recordedWith"`
}

// TestCompatHashParity is the FR-025 gate: gskill must not claim
// skills-lock.json computedHash compatibility unless every fixture matches the
// hash recorded from the reference implementation.
func TestCompatHashParity(t *testing.T) {
	t.Parallel()
	fixturesDir := filepath.Join("testdata", "compat", "fixtures")
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	if len(entries) < 8 {
		t.Fatalf("expected >= 8 fixtures, found %d", len(entries))
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join("testdata", "compat", "expected", e.Name()+".json"))
			if err != nil {
				t.Fatalf("missing recorded hash: %v", err)
			}
			var want expectedCompat
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("parse expected: %v", err)
			}
			got, err := integrity.CompatHash(filepath.Join(fixturesDir, e.Name()))
			if err != nil {
				t.Fatalf("CompatHash: %v", err)
			}
			if got != want.ComputedHash {
				t.Errorf("CompatHash = %s, want %s (%s)", got, want.ComputedHash, want.RecordedWith)
			}
		})
	}
}

// copyTree copies the basic fixture into a temp dir so runtime cases can
// mutate it.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := os.ReadFile(p) //nolint:gosec // test fixture
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
}

// TestCompatHashIgnoresEmptyDirs: empty directories contribute nothing (git
// cannot commit them, so this case runs against a runtime copy).
func TestCompatHashIgnoresEmptyDirs(t *testing.T) {
	t.Parallel()
	src := filepath.Join("testdata", "compat", "fixtures", "basic")
	base, err := integrity.CompatHash(src)
	if err != nil {
		t.Fatalf("CompatHash: %v", err)
	}
	tmp := t.TempDir()
	copyTree(t, src, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "empty", "nested-empty"), 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := integrity.CompatHash(tmp)
	if err != nil {
		t.Fatalf("CompatHash: %v", err)
	}
	if got != base {
		t.Errorf("empty dirs changed the hash: %s != %s", got, base)
	}
}

// TestCompatHashSkipsSymlinks: the reference implementation only hashes
// regular files; symlinks are neither followed nor hashed.
func TestCompatHashSkipsSymlinks(t *testing.T) {
	t.Parallel()
	src := filepath.Join("testdata", "compat", "fixtures", "basic")
	base, err := integrity.CompatHash(src)
	if err != nil {
		t.Fatalf("CompatHash: %v", err)
	}
	tmp := t.TempDir()
	copyTree(t, src, tmp)
	if err := os.Symlink("SKILL.md", filepath.Join(tmp, "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(".", filepath.Join(tmp, "dirlink")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := integrity.CompatHash(tmp)
	if err != nil {
		t.Fatalf("CompatHash: %v", err)
	}
	if got != base {
		t.Errorf("symlinks changed the hash: %s != %s", got, base)
	}
}

// TestCompatHashDetectsRename: the path participates in the hash.
func TestCompatHashDetectsRename(t *testing.T) {
	t.Parallel()
	src := filepath.Join("testdata", "compat", "fixtures", "basic")
	base, err := integrity.CompatHash(src)
	if err != nil {
		t.Fatalf("CompatHash: %v", err)
	}
	tmp := t.TempDir()
	copyTree(t, src, tmp)
	if err := os.Rename(filepath.Join(tmp, "reference.md"), filepath.Join(tmp, "renamed.md")); err != nil {
		t.Fatal(err)
	}
	got, err := integrity.CompatHash(tmp)
	if err != nil {
		t.Fatalf("CompatHash: %v", err)
	}
	if got == base {
		t.Error("rename did not change the hash")
	}
}
