package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/store"
)

func TestStore_GCRemovesUnreferenced(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := store.New(t.TempDir())
	keep := "sha256:keepme"
	drop := "sha256:dropme"
	if _, err := s.Put(keep, src); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(drop, src); err != nil {
		t.Fatal(err)
	}

	removed, err := s.GC(map[string]bool{keep: true})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if !s.Has(keep) {
		t.Error("referenced entry was removed")
	}
	if s.Has(drop) {
		t.Error("unreferenced entry survived GC")
	}
}

func TestStore_GCEmptyStore(t *testing.T) {
	t.Parallel()

	s := store.New(filepath.Join(t.TempDir(), "absent"))
	if removed, err := s.GC(nil); err != nil || removed != 0 {
		t.Errorf("GC on empty store = (%d, %v), want (0, nil)", removed, err)
	}
}
