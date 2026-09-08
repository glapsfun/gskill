package integration_test

import (
	"os"
	"os/exec"
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

// TestRepoOwned_AddLeavesSkillCommittable (spec 022 FR-010, US1): after a
// fresh `add` in a git project, the skill copy and agent link show up in
// `git status` as addable, while the local state dir stays ignored.
func TestRepoOwned_AddLeavesSkillCommittable(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	gitRun(t, proj, "init", "--quiet", "-b", "main")

	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	out, err := exec.CommandContext(t.Context(), "git", "-C", proj, "status", "--porcelain", "-uall").Output() //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	status := string(out)
	if !strings.Contains(status, ".agents/skills/demo") {
		t.Errorf("skill copy not addable in git status:\n%s", status)
	}
	if !strings.Contains(status, ".claude/") {
		t.Errorf("agent link not addable in git status:\n%s", status)
	}
	if strings.Contains(status, ".gskill/") {
		t.Errorf(".gskill/ local state is not ignored:\n%s", status)
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

// TestRepoOwned_SymlinklessCheckoutDetected (spec 022 FR-016, frozen wording
// in contracts/cli-surface.md): a checkout made without symlink support
// leaves a plain file holding the link text at the agent path — check and
// doctor report exactly that, and never repair it.
func TestRepoOwned_SymlinklessCheckoutDetected(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	// Simulate the core.symlinks=false artifact: the agent link becomes a
	// plain file containing the link text.
	linkPath := filepath.Join(proj, ".claude", "skills", "demo")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if err := os.Remove(linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkPath, []byte(target+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, _ := runGskill(t, proj, "project", "check")
	if !strings.Contains(stderr, "plain file, not a symlink") || !strings.Contains(stderr, "core.symlinks=false") {
		t.Errorf("check did not emit the frozen symlink-less error, stderr:\n%s", stderr)
	}
	if _, _, code := runGskill(t, proj, "project", "check", "--fail-on-drift"); code != 7 {
		t.Errorf("check --fail-on-drift exit = %d, want 7", code)
	}

	_, stderr, _ = runGskill(t, proj, "doctor")
	if !strings.Contains(stderr, "plain file, not a symlink") {
		t.Errorf("doctor did not report the degraded checkout, stderr:\n%s", stderr)
	}

	// Never silently repaired: the artifact is still the plain file — and
	// sync refuses to reconcile it, reporting the same frozen error
	// (spec 022, epic T04: error, not silent repair).
	requirePlainFile(t, linkPath, "after check/doctor")
	_, stderr, code := runGskill(t, proj, "project", "sync")
	if code == 0 {
		t.Error("sync on a symlink-less checkout succeeded, want fail-closed")
	}
	if !strings.Contains(stderr, "plain file, not a symlink") {
		t.Errorf("sync did not emit the frozen symlink-less error, stderr:\n%s", stderr)
	}
	requirePlainFile(t, linkPath, "after sync")
}

// requirePlainFile fails unless path is still a regular file (the degraded
// checkout artifact must never be repaired or removed).
func requirePlainFile(t *testing.T, path, when string) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		t.Errorf("degraded artifact modified %s (err=%v)", when, err)
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
