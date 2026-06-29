package active_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/active"
)

// newStoreRoot returns a fresh store-root directory.
func newStoreRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("mkdir store root: %v", err)
	}
	return root
}

// makeStore creates store content for name under storeRoot and returns its path.
func makeStore(t *testing.T, storeRoot, name string) string {
	t.Helper()
	dir := filepath.Join(storeRoot, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# skill\n"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	return dir
}

func TestEnsureActive_CreatesSymlinkIntoStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")

	got, err := active.EnsureActive(root, "argocd", storePath, sr)
	if err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if want := active.Path(root, "argocd"); got != want {
		t.Errorf("active path = %q, want %q", got, want)
	}
	info, err := os.Lstat(got)
	if err != nil {
		t.Fatalf("lstat active: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("active entry is not a symlink")
	}
	if _, err := os.Stat(filepath.Join(got, "SKILL.md")); err != nil {
		t.Errorf("active entry does not resolve to store content: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", storePath); h != active.HealthOK {
		t.Errorf("health = %q, want ok", h)
	}
}

func TestEnsureActive_Idempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")

	first, err := active.EnsureActive(root, "argocd", storePath, sr)
	if err != nil {
		t.Fatalf("first EnsureActive: %v", err)
	}
	before, err := os.Lstat(first)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	second, err := active.EnsureActive(root, "argocd", storePath, sr)
	if err != nil {
		t.Fatalf("second EnsureActive: %v", err)
	}
	if first != second {
		t.Errorf("path changed: %q != %q", first, second)
	}
	after, err := os.Lstat(second)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("idempotent EnsureActive rewrote the entry (mtime changed)")
	}
}

func TestEnsureActive_RepointsOnContentChange(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	oldStore := makeStore(t, sr, "v1")
	newStore := makeStore(t, sr, "v2")

	if _, err := active.EnsureActive(root, "argocd", oldStore, sr); err != nil {
		t.Fatalf("EnsureActive old: %v", err)
	}
	if _, err := active.EnsureActive(root, "argocd", newStore, sr); err != nil {
		t.Fatalf("EnsureActive new: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", newStore); h != active.HealthOK {
		t.Errorf("health after re-point = %q, want ok", h)
	}
}

func TestEnsureActive_FailsClosedOnForeignSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")

	// A foreign symlink pointing outside the store occupies the active path.
	foreign := t.TempDir()
	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(foreign, dest); err != nil {
		t.Fatalf("symlink foreign: %v", err)
	}

	if _, err := active.EnsureActive(root, "argocd", storePath, sr); err == nil {
		t.Fatal("expected fail-closed on a foreign active symlink")
	}
	// The foreign symlink is left intact.
	if got, _ := os.Readlink(dest); got != foreign {
		t.Errorf("foreign symlink was modified: %q", got)
	}
}

func TestEnsureActive_FailsClosedOnForeignDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")

	// A foreign directory with different content occupies the active path.
	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatalf("mkdir foreign dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("# foreign\n"), 0o600); err != nil {
		t.Fatalf("write foreign: %v", err)
	}

	if _, err := active.EnsureActive(root, "argocd", storePath, sr); err == nil {
		t.Fatal("expected fail-closed on a foreign active directory")
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "SKILL.md")); string(got) != "# foreign\n" { //nolint:gosec // test reads its own temp dir
		t.Errorf("foreign dir content was modified: %q", got)
	}
}

func TestEnsureActive_AcceptsMatchingCopy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")

	// A real directory whose content matches the store is a managed copy (or
	// identical content) and is accepted idempotently, not destroyed.
	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("# skill\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := active.EnsureActive(root, "argocd", storePath, sr); err != nil {
		t.Errorf("EnsureActive on a matching copy should succeed: %v", err)
	}
}

func TestHealthOf_States(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")

	if h, _ := active.HealthOf(root, "argocd", storePath); h != active.HealthMissing {
		t.Errorf("missing health = %q, want missing", h)
	}
	if _, err := active.EnsureActive(root, "argocd", storePath, sr); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", storePath); h != active.HealthOK {
		t.Errorf("ok health = %q, want ok", h)
	}

	// Broken: remove the store target out from under the link.
	if err := os.RemoveAll(storePath); err != nil {
		t.Fatalf("rm store: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", storePath); h != active.HealthBroken {
		t.Errorf("broken health = %q, want broken", h)
	}

	// Foreign: a plain directory occupying a different name.
	foreign := active.Path(root, "foreign")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatalf("mkdir foreign: %v", err)
	}
	if h, _ := active.HealthOf(root, "foreign", storePath); h != active.HealthForeign {
		t.Errorf("foreign health = %q, want foreign", h)
	}
}

func TestRemove_OnlyManagedSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")

	// Missing → no-op.
	if err := active.Remove(root, "argocd"); err != nil {
		t.Fatalf("Remove missing: %v", err)
	}
	// Managed symlink → removed.
	if _, err := active.EnsureActive(root, "argocd", storePath, sr); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := active.Remove(root, "argocd"); err != nil {
		t.Fatalf("Remove managed: %v", err)
	}
	if _, err := os.Lstat(active.Path(root, "argocd")); !os.IsNotExist(err) {
		t.Errorf("managed active entry not removed")
	}

	// Foreign dir → left intact.
	foreign := active.Path(root, "foreign")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatalf("mkdir foreign: %v", err)
	}
	if err := active.Remove(root, "foreign"); err != nil {
		t.Fatalf("Remove foreign: %v", err)
	}
	if _, err := os.Lstat(foreign); err != nil {
		t.Errorf("foreign dir was removed: %v", err)
	}
}

func TestList_ReturnsManagedNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sr := newStoreRoot(t)
	storePath := makeStore(t, sr, "argocd")
	if _, err := active.EnsureActive(root, "argocd", storePath, sr); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := os.MkdirAll(active.Path(root, "foreign"), 0o750); err != nil {
		t.Fatalf("mkdir foreign: %v", err)
	}
	names, err := active.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 1 || names[0] != "argocd" {
		t.Errorf("List = %v, want [argocd]", names)
	}
}
