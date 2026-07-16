package fsutil_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/fsutil"
)

func TestWriteFileAtomic_WritesContentAndLeavesNoTemp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	want := []byte("hello gskill\n")

	if err := fsutil.WriteFileAtomic(path, want, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q", got, want)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir has %d entries %v, want only the final file (no temp leftover)", len(entries), names)
	}
}

func TestWriteFileAtomic_OverwritesExisting(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.txt")
	if err := fsutil.WriteFileAtomic(path, []byte("v1"), 0o600); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := fsutil.WriteFileAtomic(path, []byte("v2"), 0o600); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "v2" {
		t.Errorf("content = %q, want %q", got, "v2")
	}
}

func TestWriteFileAtomic_CreatesParentDirs(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "deep", "out.txt")
	if err := fsutil.WriteFileAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created in nested dirs: %v", err)
	}
}

func TestTempDir_CreatesUnderParent(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "staging")
	got, err := fsutil.TempDir(parent, "work-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if filepath.Dir(got) != parent {
		t.Errorf("temp dir %q not under parent %q", got, parent)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("temp dir not created: %v", err)
	}
}

func TestCopyDir_PreservesTreeAndExecBit(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("doc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "run.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // exec bit is the property under test
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := fsutil.CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "SKILL.md")); err != nil || string(got) != "doc" { //nolint:gosec // test-controlled path
		t.Errorf("SKILL.md = %q, err=%v, want %q", got, err, "doc")
	}
	info, err := os.Stat(filepath.Join(dst, "sub", "run.sh"))
	if err != nil {
		t.Fatalf("stat copied script: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("exec bit not preserved: mode %v", info.Mode().Perm())
	}
}

func TestSymlinkOrCopy_SymlinksWhenSupported(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("doc"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "link")

	symlinked, err := fsutil.SymlinkOrCopy(src, dst)
	if err != nil {
		t.Fatalf("SymlinkOrCopy: %v", err)
	}
	if !symlinked {
		t.Skip("filesystem does not support symlinks; copy fallback exercised elsewhere")
	}
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("dst is not a symlink: mode %v", info.Mode())
	}
	if got, err := os.ReadFile(filepath.Join(dst, "SKILL.md")); err != nil || string(got) != "doc" { //nolint:gosec // test-controlled path
		t.Errorf("read through symlink = %q, err=%v, want %q", got, err, "doc")
	}
}

func TestWriteJSONAtomic_DeterministicWithTrailingNewline(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	path := filepath.Join(t.TempDir(), "nested", "out.json")
	if err := fsutil.WriteJSONAtomic(path, payload{Name: "a", Count: 1}, 0o600); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}
	first, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || first[len(first)-1] != '\n' {
		t.Error("output does not end with a trailing newline")
	}
	// Same value re-serializes byte-identically (deterministic writes).
	if err := fsutil.WriteJSONAtomic(path, payload{Name: "a", Count: 1}, 0o600); err != nil {
		t.Fatalf("second WriteJSONAtomic: %v", err)
	}
	second, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("re-serialization differs:\n%s\nvs\n%s", first, second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestOwnerOnlyTempDir_Permissions(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "tmp")
	dir, err := fsutil.OwnerOnlyTempDir(parent, "object-abc-*")
	if err != nil {
		t.Fatalf("OwnerOnlyTempDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("temp dir perm = %o, want 0700", perm)
	}
}

func TestListStaleDirs_FindsOnlyOldEntries(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	oldDir := filepath.Join(parent, "object-aaa-1")
	newDir := filepath.Join(parent, "object-bbb-2")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldDir, past, past); err != nil {
		t.Fatal(err)
	}

	stale, err := fsutil.ListStaleDirs(parent, 24*time.Hour)
	if err != nil {
		t.Fatalf("ListStaleDirs: %v", err)
	}
	if len(stale) != 1 || stale[0] != oldDir {
		t.Errorf("ListStaleDirs = %v, want [%s]", stale, oldDir)
	}
}

func TestListStaleDirs_MissingParentIsEmpty(t *testing.T) {
	t.Parallel()

	stale, err := fsutil.ListStaleDirs(filepath.Join(t.TempDir(), "absent"), time.Hour)
	if err != nil {
		t.Fatalf("ListStaleDirs on missing parent: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("ListStaleDirs = %v, want empty", stale)
	}
}
