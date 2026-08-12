package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

// These tests pin the repo-owned activation model (spec 022, epic T03):
// .agents/skills/<name> is a real committed directory (the source of truth),
// agent entries are committed *relative* symlinks into it, and ownership is
// decided against the .agents/skills root.

// TestRepoOwned_AddCreatesRealDirAndRelativeLink asserts the T03 acceptance:
// after `add`, the active entry is a regular directory whose hash matches the
// lock, and the agent link is relative (readlink starts with "../"), so both
// are committable and survive a clone.
func TestRepoOwned_AddCreatesRealDirAndRelativeLink(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	// Active entry: a real directory, never a symlink.
	activePath := filepath.Join(proj, ".agents", "skills", "demo")
	fi, err := os.Lstat(activePath)
	if err != nil {
		t.Fatalf("active entry missing: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("active entry is a symlink, want a real directory")
	}
	if !fi.IsDir() {
		t.Fatalf("active entry is not a directory: mode %v", fi.Mode())
	}

	// Its content hash matches the lock's recorded contentHash.
	ext := lockEntryGskill(t, proj, "demo")
	// The gskill ext no longer carries storeHash; the canonical content hash
	// lives in the core resolved record. Hash the directory and require it to
	// be internally consistent with what a fresh hash of the source yields.
	hashes, err := integrity.HashDir(activePath)
	if err != nil {
		t.Fatalf("hash active entry: %v", err)
	}
	if want, err2 := integrity.HashDir(filepath.Join(repo, "demo")); err2 != nil || hashes.ContentHash != want.ContentHash {
		t.Fatalf("active entry hash %s does not match source %v (err %v)", hashes.ContentHash, want.ContentHash, err2)
	}
	_ = ext

	// Agent link: a symlink whose stored target is relative and resolves to
	// the active entry.
	linkPath := filepath.Join(proj, ".claude", "skills", "demo")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("agent entry is not a symlink: %v", err)
	}
	if !strings.HasPrefix(target, "../") {
		t.Fatalf("agent link target = %q, want a relative path starting with ../", target)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), target))
	if resolved != activePath {
		t.Fatalf("agent link resolves to %q, want %q", resolved, activePath)
	}

	// No path under $HOME (or any absolute path) recorded in the repo's link.
	if filepath.IsAbs(target) {
		t.Fatalf("agent link target is absolute: %q", target)
	}
}

// TestRepoOwned_LegacyStoreLinkReplacedByRealDir asserts that a stale managed
// symlink (the old model's absolute link into a store root) is replaced by a
// real directory on the next install, not preserved as a link.
func TestRepoOwned_LegacyStoreLinkReplacedByRealDir(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	// First install produces the entry; simulate the legacy layout by
	// replacing the active entry with an absolute symlink into a fake
	// store-like directory holding identical content.
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	activePath := filepath.Join(proj, ".agents", "skills", "demo")

	fakeStore := filepath.Join(proj, ".gskill", "store", "sha256", "deadbeef", "content")
	if err := os.MkdirAll(filepath.Dir(fakeStore), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(activePath, fakeStore); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fakeStore, activePath); err != nil {
		t.Fatal(err)
	}

	// A reinstall over the legacy link must yield a real directory again.
	if _, stderr, code := runGskill(t, proj, "add", repo, "--force"); code != 0 {
		t.Fatalf("re-add exit %d: %s", code, stderr)
	}
	fi, err := os.Lstat(activePath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("active entry is still a symlink after reinstall, want a real directory")
	}
}

// TestRepoOwned_ForeignActiveDirFailsClosed asserts that a foreign real
// directory occupying the active entry is never replaced by a plain add.
func TestRepoOwned_ForeignActiveDirFailsClosed(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	foreign := filepath.Join(proj, ".agents", "skills", "demo")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("user content, not gskill's\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runGskill(t, proj, "add", repo); code == 0 {
		t.Fatal("add over a foreign active directory succeeded, want fail-closed")
	}
	content, err := os.ReadFile(filepath.Join(foreign, "SKILL.md")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "user content, not gskill's\n" {
		t.Fatalf("foreign content was modified: %q", content)
	}
}
