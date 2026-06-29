package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// patchLock rewrites gskill.lock, replacing old with new (raw string edit, used
// to simulate a malicious/malformed committed lockfile).
func patchLock(t *testing.T, proj, old, replacement string) {
	t.Helper()
	path := filepath.Join(proj, "gskill.lock")
	data := readFile(t, path)
	patched := strings.Replace(string(data), old, replacement, 1)
	if patched == string(data) {
		t.Fatalf("patchLock: %q not found in lockfile:\n%s", old, data)
	}
	if err := os.WriteFile(path, []byte(patched), 0o600); err != nil {
		t.Fatalf("write patched lock: %v", err)
	}
}

// TestAdversarial_MaliciousLockPathNotDeleted covers review F1: a lockfile target
// pointing outside the project must never be deleted by remove/prune.
func TestAdversarial_MaliciousLockPathNotDeleted(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t) // demo installed for claude + codex

	// A sentinel directory outside the project, with content that must survive.
	sentinel := t.TempDir()
	sentinelFile := filepath.Join(sentinel, "precious.txt")
	if err := os.WriteFile(sentinelFile, []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Point codex's recorded target at the sentinel directory (absolute escape).
	patchLock(t, proj, `".codex/skills/demo"`, `"`+sentinel+`"`)

	if _, stderr, code := runGskill(t, proj, "remove", "demo"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}
	// The out-of-bounds path is untouched; the valid claude target is removed.
	if _, err := os.Stat(sentinelFile); err != nil {
		t.Errorf("sentinel outside project was deleted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".claude", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("valid claude target not removed (err=%v)", err)
	}
}

// TestAdversarial_CorruptStoreFailsCheckClosed covers review F3/F4: a tampered
// store makes `check` fail closed with exit 6, not just `verify`.
func TestAdversarial_CorruptStoreFailsCheckClosed(t *testing.T) {
	t.Parallel()
	proj, _ := addShared(t)

	storePath, err := filepath.EvalSymlinks(filepath.Join(proj, ".agents", "skills", "demo"))
	if err != nil {
		t.Fatalf("eval store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storePath, "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if _, _, code := runGskill(t, proj, "check"); code != 6 {
		t.Errorf("check on corrupt store exit = %d, want 6 (fail closed)", code)
	}
}

// TestAdversarial_CorruptCopyDetectedAndRepaired covers review F5: a tampered
// copy target is detected by check and restored by repair.
func TestAdversarial_CorruptCopyDetectedAndRepaired(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude", "--copy"); code != 0 {
		t.Fatalf("add --copy: %s", stderr)
	}

	target := filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md")
	if err := os.WriteFile(target, []byte("# tampered copy\n"), 0o600); err != nil {
		t.Fatalf("tamper copy: %v", err)
	}

	if _, _, code := runGskill(t, proj, "check"); code != 6 {
		t.Errorf("check on corrupt copy exit = %d, want 6", code)
	}
	if _, stderr, code := runGskill(t, proj, "repair"); code != 0 {
		t.Fatalf("repair: %s", stderr)
	}
	if got := string(readFile(t, target)); strings.Contains(got, "tampered") {
		t.Errorf("repair did not restore the corrupt copy:\n%s", got)
	}
	if _, stderr, code := runGskill(t, proj, "check", "--fail-on-drift"); code != 0 {
		t.Errorf("check after repair exit %d: %s", code, stderr)
	}
}

// TestAdversarial_ForeignActiveEntryFailsClosed covers review F2: a foreign
// directory at the active path is never overwritten; sync fails closed (exit 3).
func TestAdversarial_ForeignActiveEntryFailsClosed(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Replace the managed active entry with a foreign directory.
	activeEntry := filepath.Join(proj, ".agents", "skills", "demo")
	if err := os.Remove(activeEntry); err != nil {
		t.Fatalf("rm active: %v", err)
	}
	if err := os.MkdirAll(activeEntry, 0o750); err != nil {
		t.Fatalf("mkdir foreign: %v", err)
	}
	foreignFile := filepath.Join(activeEntry, "MINE.txt")
	if err := os.WriteFile(foreignFile, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runGskill(t, proj, "sync"); code != 3 {
		t.Errorf("sync over foreign active entry exit = %d, want 3 (fail closed)", code)
	}
	if _, err := os.Stat(foreignFile); err != nil {
		t.Errorf("foreign active content was destroyed: %v", err)
	}
}

// TestAdversarial_AgentAddOffline covers review F7/F6: adding an agent reuses the
// lock with no resolution (works after the source is gone) and does not rewrite
// the existing agent's target.
func TestAdversarial_AgentAddOffline(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude"); code != 0 {
		t.Fatalf("add claude: %s", stderr)
	}

	claudeTarget := filepath.Join(proj, ".claude", "skills", "demo")
	before, err := os.Lstat(claudeTarget)
	if err != nil {
		t.Fatalf("lstat claude: %v", err)
	}

	// Make the source unavailable: any resolve/fetch would now fail.
	if err := os.RemoveAll(repo); err != nil {
		t.Fatalf("rm repo: %v", err)
	}

	if _, stderr, code := runGskill(t, proj, "add", repo, "--skill", "demo", "--agent", "codex"); code != 0 {
		t.Fatalf("offline agent-add failed (should not resolve): %s", stderr)
	}
	requireResolvesActive(t, proj, ".codex", "demo")
	after, err := os.Lstat(claudeTarget)
	if err != nil {
		t.Fatalf("lstat claude after: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("existing claude target was rewritten by an agent-add")
	}
}
