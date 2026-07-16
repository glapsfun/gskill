package globalstore_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/home"
	"github.com/glapsfun/gskill/internal/integrity"
)

// newTestHome returns an ensured Home rooted in a temp dir.
func newTestHome(t *testing.T) *home.Home {
	t.Helper()
	h := home.New(filepath.Join(t.TempDir(), "gskill-home"))
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure home: %v", err)
	}
	return h
}

// writeSkillDir creates a small skill tree and returns its dir and canonical hash.
func writeSkillDir(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "notes.md"), []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hashes, err := integrity.HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir: %v", err)
	}
	return dir, hashes.ContentHash
}

func TestStore_PathAndHas(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)

	key := "sha256:" + "ab12" + "00000000000000000000000000000000000000000000000000000000cd34"
	objPath := s.ObjectPath(key)
	want := filepath.Join(h.StoreDir(), "sha256", key[len("sha256:"):])
	if objPath != want {
		t.Errorf("ObjectPath = %q, want %q", objPath, want)
	}
	if got := s.ContentPath(key); got != filepath.Join(want, "content") {
		t.Errorf("ContentPath = %q, want %q", got, filepath.Join(want, "content"))
	}
	if s.Has(key) {
		t.Error("Has = true for absent object")
	}
}

func TestStore_OpenAbsentReturnsNotFound(t *testing.T) {
	t.Parallel()

	s := globalstore.New(newTestHome(t))
	_, err := s.Open("sha256:deadbeef")
	if err == nil {
		t.Fatal("Open absent object: want error")
	}
	if !errors.Is(err, globalstore.ErrObjectNotFound) {
		t.Errorf("Open error = %v, want ErrObjectNotFound", err)
	}
}

func TestStore_OpenAdmittedObject(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# argocd v1\n")

	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{
		SourceType: "github",
		Source:     "github.com/example/skills",
		SkillPath:  "skills/argocd",
		Version:    "1.4.0",
		Commit:     "aaa111",
	}); err != nil {
		t.Fatalf("Admit: %v", err)
	}

	obj, err := s.Open(key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if obj.Key != key {
		t.Errorf("obj.Key = %q, want %q", obj.Key, key)
	}
	if obj.Metadata.ContentHash != key {
		t.Errorf("metadata contentHash = %q, want %q", obj.Metadata.ContentHash, key)
	}
	if !s.Has(key) {
		t.Error("Has = false after admission")
	}
	if _, err := os.Stat(filepath.Join(s.ContentPath(key), "SKILL.md")); err != nil {
		t.Errorf("admitted content missing SKILL.md: %v", err)
	}
}

func TestStore_ListKeys(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src1, key1 := writeSkillDir(t, "# one\n")
	src2, key2 := writeSkillDir(t, "# two\n")
	for src, key := range map[string]string{src1: key1, src2: key2} {
		if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"}); err != nil {
			t.Fatalf("Admit: %v", err)
		}
	}

	keys, err := s.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListKeys = %v, want 2 keys", keys)
	}
	found := map[string]bool{}
	for _, k := range keys {
		found[k] = true
	}
	if !found[key1] || !found[key2] {
		t.Errorf("ListKeys = %v, want %q and %q", keys, key1, key2)
	}
}
