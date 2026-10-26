package integrity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

func writeFile(t *testing.T, dir, rel string, data []byte, perm os.FileMode) {
	t.Helper()

	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, perm); err != nil {
		t.Fatal(err)
	}
}

func TestHashDir_DeterministicAndPathSorted(t *testing.T) {
	t.Parallel()

	mk := func() string {
		d := t.TempDir()
		writeFile(t, d, "SKILL.md", []byte("# skill\n"), 0o600)
		writeFile(t, d, "b/second.txt", []byte("two\n"), 0o600)
		writeFile(t, d, "a/first.txt", []byte("one\n"), 0o600)
		return d
	}

	h1, err := integrity.HashDir(mk())
	if err != nil {
		t.Fatalf("HashDir: %v", err)
	}
	h2, err := integrity.HashDir(mk())
	if err != nil {
		t.Fatalf("HashDir: %v", err)
	}
	if h1.ContentHash != h2.ContentHash {
		t.Errorf("content hash not deterministic: %s vs %s", h1.ContentHash, h2.ContentHash)
	}
	if !strings.HasPrefix(h1.ContentHash, "sha256:") {
		t.Errorf("content hash missing sha256: prefix: %s", h1.ContentHash)
	}
}

func TestHashDir_LFNormalizesText(t *testing.T) {
	t.Parallel()

	lf := t.TempDir()
	writeFile(t, lf, "SKILL.md", []byte("line1\nline2\n"), 0o600)

	crlf := t.TempDir()
	writeFile(t, crlf, "SKILL.md", []byte("line1\r\nline2\r\n"), 0o600)

	hLF, _ := integrity.HashDir(lf)
	hCRLF, _ := integrity.HashDir(crlf)
	if hLF.ContentHash != hCRLF.ContentHash {
		t.Errorf("CRLF/LF produced different hashes: %s vs %s", hLF.ContentHash, hCRLF.ContentHash)
	}
}

// TestHashDir_ExecBitDoesNotChangeHash pins spec 028: permissions are not
// content, so a filesystem that reports every file as executable (WSL DrvFs)
// yields the same identity. Only the pre-028 legacy value still sees the bit.
func TestHashDir_ExecBitDoesNotChangeHash(t *testing.T) {
	t.Parallel()

	blob := []byte{0x00, 0x0d, 0x0a, 0x01}
	mk := func(mode os.FileMode) string {
		d := t.TempDir()
		writeFile(t, d, "SKILL.md", []byte("x"), 0o600)
		writeFile(t, d, "run.sh", []byte("#!/bin/sh\n"), 0o600)
		writeFile(t, d, "blob.bin", blob, 0o600)
		for _, rel := range []string{"run.sh", "blob.bin"} {
			if err := os.Chmod(filepath.Join(d, rel), mode); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}

	hp, err := integrity.HashDir(mk(0o644))
	if err != nil {
		t.Fatal(err)
	}
	he, err := integrity.HashDir(mk(0o755))
	if err != nil {
		t.Fatal(err)
	}
	if hp.ContentHash != he.ContentHash {
		t.Errorf("exec bit changed the content hash: %s vs %s", hp.ContentHash, he.ContentHash)
	}
	if hp.LegacyContentHash == he.LegacyContentHash {
		t.Error("legacy hash ignored the exec bit; it must keep the pre-028 rule")
	}
}

func TestHashDir_SymlinkAndFileStillDiffer(t *testing.T) {
	t.Parallel()

	file := t.TempDir()
	writeFile(t, file, "SKILL.md", []byte("x"), 0o600)
	writeFile(t, file, "ref.md", []byte(integrity.SkillFileName), 0o600)

	link := t.TempDir()
	writeFile(t, link, "SKILL.md", []byte("x"), 0o600)
	if err := os.Symlink(integrity.SkillFileName, filepath.Join(link, "ref.md")); err != nil {
		t.Fatal(err)
	}

	hf, err := integrity.HashDir(file)
	if err != nil {
		t.Fatal(err)
	}
	hl, err := integrity.HashDir(link)
	if err != nil {
		t.Fatal(err)
	}
	if hf.ContentHash == hl.ContentHash {
		t.Error("a symlink and a regular file with the same bytes produced the same hash")
	}
}

func TestHashDir_BinaryNotLFNormalized(t *testing.T) {
	t.Parallel()

	a := t.TempDir()
	writeFile(t, a, "SKILL.md", []byte("x"), 0o600)
	writeFile(t, a, "blob.bin", []byte{0x00, 0x0d, 0x0a, 0x01}, 0o600)

	b := t.TempDir()
	writeFile(t, b, "SKILL.md", []byte("x"), 0o600)
	writeFile(t, b, "blob.bin", []byte{0x00, 0x0a, 0x01}, 0o600)

	ha, _ := integrity.HashDir(a)
	hb, _ := integrity.HashDir(b)
	if ha.ContentHash == hb.ContentHash {
		t.Error("binary CRLF was normalized; bytes must be hashed verbatim")
	}
}

func TestHashDir_SkillFileHashMatchesContent(t *testing.T) {
	t.Parallel()

	d := t.TempDir()
	body := []byte("# heading\n")
	writeFile(t, d, "SKILL.md", body, 0o600)
	writeFile(t, d, "other.txt", []byte("ignored for skill-file hash\n"), 0o600)

	h, err := integrity.HashDir(d)
	if err != nil {
		t.Fatalf("HashDir: %v", err)
	}
	if h.SkillFileHash != integrity.HashContent(body) {
		t.Errorf("SkillFileHash = %s, want %s", h.SkillFileHash, integrity.HashContent(body))
	}
}

func TestHashContent_StablePrefix(t *testing.T) {
	t.Parallel()

	if got := integrity.HashContent([]byte("abc")); !strings.HasPrefix(got, "sha256:") {
		t.Errorf("HashContent = %q, want sha256: prefix", got)
	}
}
