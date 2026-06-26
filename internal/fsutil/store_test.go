package fsutil_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/fsutil"
)

func TestKeyPath_SplitsAlgoPrefix(t *testing.T) {
	t.Parallel()

	got := fsutil.KeyPath("/root", "sha256:abcd")
	want := filepath.Join("/root", "sha256", "abcd")
	if got != want {
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}

func TestImportDir_CopiesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	dest, err := fsutil.ImportDir(root, "sha256:abcd", src)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	if !strings.HasPrefix(dest, root) {
		t.Errorf("dest %q not under root %q", dest, root)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "SKILL.md")); err != nil || string(got) != "body" { //nolint:gosec // test path
		t.Errorf("imported content = %q, err=%v", got, err)
	}

	// Second import is a no-op returning the same path.
	dest2, err := fsutil.ImportDir(root, "sha256:abcd", src)
	if err != nil {
		t.Fatalf("second ImportDir: %v", err)
	}
	if dest2 != dest {
		t.Errorf("idempotent path mismatch: %q vs %q", dest2, dest)
	}
}
