package integrity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func digest(t *testing.T, root string, spec integrity.OverrideSpec) string {
	t.Helper()
	d, err := integrity.OverrideDigest(root, spec)
	if err != nil {
		t.Fatalf("OverrideDigest: %v", err)
	}
	return d
}

// TestOverrideDigest_Deterministic is the core property: identical inputs
// always yield an identical digest (FR-009).
func TestOverrideDigest_Deterministic(t *testing.T) {
	t.Parallel()

	root := writeFiles(t, map[string]string{
		"g/a.diff": "diff a\n", "g/b.diff": "diff b\n", "g/rules.md": "rules\n",
	})
	spec := integrity.OverrideSpec{
		Patch:  []string{"g/a.diff", "g/b.diff"},
		Append: []string{"g/rules.md"},
	}
	if first, second := digest(t, root, spec), digest(t, root, spec); first != second {
		t.Errorf("digest not deterministic: %q vs %q", first, second)
	}
}

// TestOverrideDigest_DeclaredOrderMatters: patches apply in sequence, so their
// declared order is data and must change the identity of the result.
func TestOverrideDigest_DeclaredOrderMatters(t *testing.T) {
	t.Parallel()

	root := writeFiles(t, map[string]string{"g/a.diff": "diff a\n", "g/b.diff": "diff b\n"})
	ab := digest(t, root, integrity.OverrideSpec{Patch: []string{"g/a.diff", "g/b.diff"}})
	ba := digest(t, root, integrity.OverrideSpec{Patch: []string{"g/b.diff", "g/a.diff"}})
	if ab == ba {
		t.Error("swapping declared patch order must change the digest")
	}
}

// TestOverrideDigest_MapOrderIrrelevant: Go map iteration order is ambient
// environment, which Constitution I forbids from reaching output. Building the
// same replace map twice must always digest identically.
func TestOverrideDigest_MapOrderIrrelevant(t *testing.T) {
	t.Parallel()

	root := writeFiles(t, map[string]string{"g/one.md": "1\n", "g/two.md": "2\n"})
	want := digest(t, root, integrity.OverrideSpec{
		Replace: map[string]string{"A.md": "g/one.md", "B.md": "g/two.md"},
	})
	// Repeat enough times that a map-order dependency would surface.
	for range 50 {
		got := digest(t, root, integrity.OverrideSpec{
			Replace: map[string]string{"B.md": "g/two.md", "A.md": "g/one.md"},
		})
		if got != want {
			t.Fatalf("digest depends on map iteration order: %q vs %q", got, want)
		}
	}
}

// TestOverrideDigest_InputContentMatters is what makes FR-010 mechanical:
// editing a referenced file changes the digest, so drift is detected with no
// file watching and no timestamps.
func TestOverrideDigest_InputContentMatters(t *testing.T) {
	t.Parallel()

	root := writeFiles(t, map[string]string{"g/rules.md": "original\n"})
	spec := integrity.OverrideSpec{Append: []string{"g/rules.md"}}
	before := digest(t, root, spec)

	if err := os.WriteFile(filepath.Join(root, "g", "rules.md"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if after := digest(t, root, spec); after == before {
		t.Error("editing a referenced override input must change the digest")
	}
}

// TestOverrideDigest_PathMatters: two identical files at different paths are
// different declarations, because the path is part of the intent.
func TestOverrideDigest_PathMatters(t *testing.T) {
	t.Parallel()

	root := writeFiles(t, map[string]string{"g/x.md": "same\n", "g/y.md": "same\n"})
	x := digest(t, root, integrity.OverrideSpec{Append: []string{"g/x.md"}})
	y := digest(t, root, integrity.OverrideSpec{Append: []string{"g/y.md"}})
	if x == y {
		t.Error("identical content at different paths must digest differently")
	}
}

// TestOverrideDigest_Empty: no declaration means no digest, which is what keeps
// spec 022 entries valid and unrewritten (FR-008).
func TestOverrideDigest_Empty(t *testing.T) {
	t.Parallel()

	if got := digest(t, t.TempDir(), integrity.OverrideSpec{}); got != "" {
		t.Errorf("empty spec digest = %q, want empty", got)
	}
}

// TestOverrideDigest_MissingInput fails closed rather than digesting a
// declaration it could not fully read.
func TestOverrideDigest_MissingInput(t *testing.T) {
	t.Parallel()

	_, err := integrity.OverrideDigest(t.TempDir(), integrity.OverrideSpec{Append: []string{"g/nope.md"}})
	if err == nil {
		t.Error("missing override input must be an error, not a silent digest")
	}
}
