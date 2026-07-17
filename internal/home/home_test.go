package home_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/glapsfun/gskill/internal/home"
)

func TestDir_DefaultUnderUserHome(t *testing.T) {
	t.Setenv(home.EnvHome, "")

	dir, err := home.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(userHome, ".gskill")
	if dir != want {
		t.Errorf("Dir = %q, want %q", dir, want)
	}
}

func TestDir_EnvOverrideWins(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-home")
	t.Setenv(home.EnvHome, custom)

	dir, err := home.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != custom {
		t.Errorf("Dir = %q, want override %q", dir, custom)
	}
}

func TestHome_LayoutAccessors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	h := home.New(root)

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Root", h.Root(), root},
		{"StoreDir", h.StoreDir(), filepath.Join(root, "store")},
		{"CacheDir", h.CacheDir(), filepath.Join(root, "cache")},
		{"TmpDir", h.TmpDir(), filepath.Join(root, "tmp")},
		{"LocksDir", h.LocksDir(), filepath.Join(root, "locks")},
		{"ProjectsDir", h.ProjectsDir(), filepath.Join(root, "projects")},
		{"PinsDir", h.PinsDir(), filepath.Join(root, "pins")},
		{"QuarantineDir", h.QuarantineDir(), filepath.Join(root, "quarantine")},
		{"ConfigFile", h.ConfigFile(), filepath.Join(root, "config.toml")},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestEnsure_CreatesOwnerOnlyTree(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "gskill-home")
	h := home.New(root)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	for _, dir := range []string{
		h.Root(), h.StoreDir(), h.CacheDir(), h.TmpDir(),
		h.LocksDir(), h.ProjectsDir(), h.PinsDir(), h.QuarantineDir(),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("%s perm = %o, want 0700", dir, perm)
		}
	}
}

func TestEnsure_Idempotent(t *testing.T) {
	t.Parallel()

	h := home.New(filepath.Join(t.TempDir(), "gskill-home"))
	if err := h.Ensure(); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
}

func TestCheckPerms_CleanTreeHasNoFindings(t *testing.T) {
	t.Parallel()

	h := home.New(filepath.Join(t.TempDir(), "gskill-home"))
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	findings, err := h.CheckPerms()
	if err != nil {
		t.Fatalf("CheckPerms: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("CheckPerms = %v, want no findings on a fresh tree", findings)
	}
}

func TestCheckPerms_FlagsGroupAndWorldWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	t.Parallel()

	h := home.New(filepath.Join(t.TempDir(), "gskill-home"))
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := os.Chmod(h.StoreDir(), 0o777); err != nil { //nolint:gosec // intentional non-restrictive perms for the test
		t.Fatalf("chmod: %v", err)
	}

	findings, err := h.CheckPerms()
	if err != nil {
		t.Fatalf("CheckPerms: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("CheckPerms found nothing for a world-writable store dir")
	}
	found := false
	for _, f := range findings {
		if f.Path == h.StoreDir() {
			found = true
			if f.Remedy == "" {
				t.Error("finding has no remediation hint")
			}
		}
	}
	if !found {
		t.Errorf("no finding for %s in %v", h.StoreDir(), findings)
	}
}

func TestCheckPathOwnership_OwnedPathOK(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	h := home.New(root)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := h.CheckPathSafety(h.StoreDir()); err != nil {
		t.Errorf("CheckPathSafety on owned 0700 dir: %v", err)
	}
}

func TestCheckPathSafety_WorldWritableRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	t.Parallel()

	root := t.TempDir()
	h := home.New(root)
	if err := h.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	dir := filepath.Join(h.StoreDir(), "unsafe")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o707); err != nil { //nolint:gosec // intentional non-restrictive perms for the test
		t.Fatal(err)
	}
	if err := h.CheckPathSafety(dir); err == nil {
		t.Error("CheckPathSafety accepted a world-writable path")
	}
}
