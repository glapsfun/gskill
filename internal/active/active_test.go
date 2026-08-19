package active_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/integrity"
)

// makeContent creates a skill content directory with the given body and
// returns its path and content hash.
func makeContent(t *testing.T, name, body string) (dir, hash string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir content: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	h, err := integrity.HashDir(dir)
	if err != nil {
		t.Fatalf("hash content: %v", err)
	}
	return dir, h.ContentHash
}

func TestEnsureActive_CreatesRealDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	got, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash})
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
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("active entry is a symlink, want a real directory")
	}
	if !info.IsDir() {
		t.Fatalf("active entry is not a directory: mode %v", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(got, "SKILL.md")); err != nil {
		t.Errorf("active entry has no content: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", hash); h != active.HealthOK {
		t.Errorf("health = %q, want ok", h)
	}
}

func TestEnsureActive_Idempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	first, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash})
	if err != nil {
		t.Fatalf("first EnsureActive: %v", err)
	}
	before, err := os.Lstat(first)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	second, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash})
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

func TestEnsureActive_ReplacesPriorVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	v1, v1hash := makeContent(t, "v1", "# v1\n")
	v2, v2hash := makeContent(t, "v2", "# v2\n")

	if _, err := active.EnsureActive(root, "argocd", v1, active.EnsureOptions{ExpectedHash: v1hash}); err != nil {
		t.Fatalf("EnsureActive v1: %v", err)
	}
	// The old version's hash is gskill-owned: the entry is replaced.
	if _, err := active.EnsureActive(root, "argocd", v2, active.EnsureOptions{
		ExpectedHash: v2hash,
		AcceptHashes: []string{v1hash},
	}); err != nil {
		t.Fatalf("EnsureActive v2: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", v2hash); h != active.HealthOK {
		t.Errorf("health after version switch = %q, want ok", h)
	}
}

func TestEnsureActive_ReplacesLegacyStoreLink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	// A pre-022 layout: the active entry is an absolute symlink into a store
	// root. It is a stale managed entry and is replaced by the real copy.
	storeRoot := filepath.Join(t.TempDir(), "store")
	obj := filepath.Join(storeRoot, "sha256", "old", "content")
	if err := os.MkdirAll(obj, 0o750); err != nil {
		t.Fatal(err)
	}
	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(obj, dest); err != nil {
		t.Fatal(err)
	}

	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{
		ExpectedHash: hash,
		LegacyRoots:  []string{storeRoot},
	}); err != nil {
		t.Fatalf("EnsureActive over legacy link: %v", err)
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("legacy link not replaced by a real directory: mode %v", info.Mode())
	}
}

func TestEnsureActive_FailsClosedOnForeignSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	foreign := t.TempDir()
	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(foreign, dest); err != nil {
		t.Fatalf("symlink foreign: %v", err)
	}

	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{
		ExpectedHash: hash,
		Replace:      true, // even reconcile paths never replace a foreign symlink
		LegacyRoots:  []string{filepath.Join(t.TempDir(), "store")},
	}); err == nil {
		t.Fatal("expected fail-closed on a foreign active symlink")
	}
	if got, _ := os.Readlink(dest); got != foreign {
		t.Errorf("foreign symlink was modified: %q", got)
	}
}

func TestEnsureActive_FailsClosedOnDriftedDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatalf("mkdir foreign dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("# foreign\n"), 0o600); err != nil {
		t.Fatalf("write foreign: %v", err)
	}

	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash}); err == nil {
		t.Fatal("expected fail-closed on unrecognized directory content")
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "SKILL.md")); string(got) != "# foreign\n" { //nolint:gosec // test reads its own temp dir
		t.Errorf("foreign dir content was modified: %q", got)
	}
}

func TestEnsureActive_ReplaceFlagReconcilesDrift(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("# drifted\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{
		ExpectedHash: hash,
		Replace:      true,
	}); err != nil {
		t.Fatalf("reconcile with Replace: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", hash); h != active.HealthOK {
		t.Errorf("health after reconcile = %q, want ok", h)
	}
}

func TestEnsureActive_AcceptsMatchingContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	dest := active.Path(root, "argocd")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("# skill\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Errorf("EnsureActive on matching content should succeed: %v", err)
	}
}

func TestEnsureActive_RejectsWrongHash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, _ := makeContent(t, "argocd", "# skill\n")

	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{
		ExpectedHash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}); err == nil {
		t.Fatal("expected integrity failure when staged content does not match the expected hash")
	}
	if _, err := os.Lstat(active.Path(root, "argocd")); !os.IsNotExist(err) {
		t.Error("a failed verified copy left an entry behind")
	}
}

func TestHealthOf_States(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	if h, _ := active.HealthOf(root, "argocd", hash); h != active.HealthMissing {
		t.Errorf("missing health = %q, want missing", h)
	}
	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if h, _ := active.HealthOf(root, "argocd", hash); h != active.HealthOK {
		t.Errorf("ok health = %q, want ok", h)
	}

	// Drifted: hand-edit the committed content.
	if err := os.WriteFile(filepath.Join(active.Path(root, "argocd"), "SKILL.md"), []byte("# edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, _ := active.HealthOf(root, "argocd", hash); h != active.HealthDrifted {
		t.Errorf("drifted health = %q, want drifted", h)
	}

	// Legacy: a symlink into a known legacy store root.
	storeRoot := filepath.Join(t.TempDir(), "store")
	obj := filepath.Join(storeRoot, "sha256", "x", "content")
	if err := os.MkdirAll(obj, 0o750); err != nil {
		t.Fatal(err)
	}
	legacy := active.Path(root, "legacy")
	if err := os.Symlink(obj, legacy); err != nil {
		t.Fatal(err)
	}
	if h, _ := active.HealthOf(root, "legacy", hash, storeRoot); h != active.HealthLegacy {
		t.Errorf("legacy health = %q, want legacy", h)
	}
	// The same link without the legacy root declared is foreign.
	if h, _ := active.HealthOf(root, "legacy", hash); h != active.HealthForeign {
		t.Errorf("undeclared legacy health = %q, want foreign", h)
	}

	// Foreign: a plain file occupying an entry.
	if err := os.WriteFile(active.Path(root, "plainfile"), []byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, _ := active.HealthOf(root, "plainfile", hash); h != active.HealthForeign {
		t.Errorf("plain-file health = %q, want foreign", h)
	}
}

func TestRemove_OnlyProvenContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")

	// Missing → no-op.
	if err := active.Remove(root, "argocd", hash); err != nil {
		t.Fatalf("Remove missing: %v", err)
	}

	// Managed directory matching the recorded hash → removed.
	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := active.Remove(root, "argocd", hash); err != nil {
		t.Fatalf("Remove managed: %v", err)
	}
	if _, err := os.Lstat(active.Path(root, "argocd")); !os.IsNotExist(err) {
		t.Errorf("managed active entry not removed")
	}

	// A symlink (legacy leftover) → removed.
	link := active.Path(root, "legacy")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := active.Remove(root, "legacy"); err != nil {
		t.Fatalf("Remove legacy link: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("legacy link not removed")
	}

	// A drifted/unverifiable dir → left intact.
	foreign := active.Path(root, "foreign")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatalf("mkdir foreign: %v", err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("# user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := active.Remove(root, "foreign", hash); err != nil {
		t.Fatalf("Remove foreign: %v", err)
	}
	if _, err := os.Lstat(foreign); err != nil {
		t.Errorf("unverifiable dir was removed: %v", err)
	}
}

func TestList_ReturnsDirsAndLinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src, hash := makeContent(t, "argocd", "# skill\n")
	if _, err := active.EnsureActive(root, "argocd", src, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := os.Symlink(t.TempDir(), active.Path(root, "legacy")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active.Path(root, "plainfile"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	names, err := active.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 || names[0] != "argocd" || names[1] != "legacy" {
		t.Errorf("List = %v, want [argocd legacy]", names)
	}
}

// TestEnsureActive_SwitchLeavesNoTemp: switching versions stages the copy as
// a temporary sibling then swaps atomically — the new content is live
// afterwards and no temp artifacts remain.
func TestEnsureActive_SwitchLeavesNoTemp(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v1, v1hash := makeContent(t, "v1", "# v1\n")
	v2, v2hash := makeContent(t, "v2", "# v2\n")

	if _, err := active.EnsureActive(root, "gamma", v1, active.EnsureOptions{ExpectedHash: v1hash}); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if _, err := active.EnsureActive(root, "gamma", v2, active.EnsureOptions{
		ExpectedHash: v2hash,
		AcceptHashes: []string{v1hash},
	}); err != nil {
		t.Fatalf("switch to v2: %v", err)
	}

	if h, _ := active.HealthOf(root, "gamma", v2hash); h != active.HealthOK {
		t.Errorf("health after switch = %q, want ok", h)
	}
	entries, err := os.ReadDir(active.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "gskill-switch") || strings.Contains(e.Name(), "gskill-old") {
			t.Errorf("temporary switch artifact left behind: %s", e.Name())
		}
	}
}

// TestEnsureActive_FailedSwitchLeavesOldEntry: when the switch cannot stage
// (read-only active dir), the previous entry is untouched — the project never
// observes a missing or half-updated skill.
func TestEnsureActive_FailedSwitchLeavesOldEntry(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory write permissions")
	}
	t.Parallel()

	root := t.TempDir()
	v1, v1hash := makeContent(t, "v1", "# v1\n")
	v2, v2hash := makeContent(t, "v2", "# v2\n")

	if _, err := active.EnsureActive(root, "gamma", v1, active.EnsureOptions{ExpectedHash: v1hash}); err != nil {
		t.Fatalf("activate v1: %v", err)
	}

	// Freeze the active dir: staging must fail before the old entry is touched.
	dir := active.Dir(root)
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // intentional non-restrictive perms for the test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) }) //nolint:gosec // intentional non-restrictive perms for the test

	if _, err := active.EnsureActive(root, "gamma", v2, active.EnsureOptions{
		ExpectedHash: v2hash,
		AcceptHashes: []string{v1hash},
	}); err == nil {
		t.Fatal("switch into a read-only dir should fail")
	}
	if h, _ := active.HealthOf(root, "gamma", v1hash); h != active.HealthOK {
		t.Errorf("old entry state after failed switch = %q, want ok (untouched)", h)
	}
}
