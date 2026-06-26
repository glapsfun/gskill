package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/store"
)

func TestStore_PutHasPathIdempotent(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := store.New(t.TempDir())
	const hash = "sha256:deadbeef"

	if s.Has(hash) {
		t.Fatal("Has reported present before Put")
	}
	path, err := s.Put(hash, src)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !s.Has(hash) {
		t.Error("Has reported absent after Put")
	}
	if s.Path(hash) != path {
		t.Errorf("Path = %q, want %q", s.Path(hash), path)
	}
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Errorf("content not stored: %v", err)
	}

	again, err := s.Put(hash, src)
	if err != nil || again != path {
		t.Errorf("Put not idempotent: path=%q err=%v", again, err)
	}
}
