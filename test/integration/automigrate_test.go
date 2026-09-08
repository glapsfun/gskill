package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// These tests pin spec 022 US4: any mutating command transparently converts a
// legacy (pre-022, home-store) project to the repo-owned layout with exactly
// one notice line, never touching the old store's bytes. Carve-outs: frozen
// installs convert links/state but leave the lockfile byte-identical, and
// skills being removed are removed directly with no content conversion.

// digestDir hashes a tree (paths, link targets, file bytes) so before/after
// comparisons can prove nothing changed.
func digestDir(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		h.Write([]byte(rel))
		if d.Type()&fs.ModeSymlink != 0 {
			target, rErr := os.Readlink(path)
			if rErr != nil {
				return rErr
			}
			h.Write([]byte(target))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, rErr := os.ReadFile(path) //nolint:gosec // test-controlled path
		if rErr != nil {
			return rErr
		}
		h.Write(data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// noticeApp builds an App with a private home and a captured notice stream.
func noticeApp(home string) (*app.App, *bytes.Buffer) {
	var buf bytes.Buffer
	return app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: home,
		Notice:     &buf,
	}), &buf
}

// makeLegacyProject installs a skill the modern way, then rewrites the
// project into the pre-022 shape: content moved into the fake home store,
// the active entry an absolute symlink into it, the agent link absolute, the
// lock entry carrying scope+storeHash instead of contentHash, and a v1
// state.json.
func makeLegacyProject(t *testing.T, a *app.App, home, proj, repo string) (hash string) {
	t.Helper()
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo); code != 0 {
		t.Fatalf("seed add exit %d: %s", code, stderr)
	}
	hash, _ = lockEntryGskill(t, proj, "demo")["contentHash"].(string)
	if hash == "" {
		t.Fatal("seed install recorded no contentHash")
	}
	hex := strings.TrimPrefix(hash, "sha256:")

	// Content moves into the old home store; the active entry becomes an
	// absolute symlink into it (the pre-022 layout).
	obj := filepath.Join(home, "store", "sha256", hex, "content")
	if err := os.MkdirAll(filepath.Dir(obj), 0o750); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(proj, ".agents", "skills", "demo")
	if err := os.Rename(entry, obj); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(obj, entry); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(proj, ".claude", "skills", "demo")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(entry)
	if err := os.Symlink(abs, link); err != nil {
		t.Fatal(err)
	}

	// The lock entry regains its pre-022 store fields.
	lockPath := filepath.Join(proj, "skills-lock.json")
	lock := string(readFile(t, lockPath))
	lock = strings.Replace(lock, `"contentHash"`, `"storeHash"`, 1)
	lock = strings.Replace(lock, `"installMode"`, `"scope": "project",`+"\n        "+`"installMode"`, 1)
	if err := os.WriteFile(lockPath, []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}

	// v1 state.json with store fields.
	state := `{"schemaVersion":1,"projectId":"p-legacy","skills":{"demo":{"storeHash":"` + hash + `","storeScope":"global","activeTarget":".agents/skills/demo","agents":{"claude":{"target":".claude/skills/demo","mode":"symlink"}}}}}`
	if err := os.WriteFile(filepath.Join(proj, ".gskill", "state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	return hash
}

// assertRepoOwnedDemo asserts the T014 end state: real dir, relative link,
// clean records.
func assertRepoOwnedDemo(t *testing.T, proj string) {
	t.Helper()
	entry := filepath.Join(proj, ".agents", "skills", "demo")
	fi, err := os.Lstat(entry)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		t.Errorf("active entry not a real directory after migration (err=%v)", err)
	}
	target, err := os.Readlink(filepath.Join(proj, ".claude", "skills", "demo"))
	if err != nil || !strings.HasPrefix(target, "../") {
		t.Errorf("agent link not relative after migration: %q (err=%v)", target, err)
	}
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	for _, dead := range []string{`"storeHash"`, `"scope"`} {
		if strings.Contains(lock, dead) {
			t.Errorf("lock still carries %s after migration:\n%s", dead, lock)
		}
	}
}

// TestAutoMigrate_SyncConvertsWithOneNotice: a mutating command converts the
// whole project, emits exactly one notice line, and leaves the old store
// byte-identical.
func TestAutoMigrate_SyncConvertsWithOneNotice(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a, notice := noticeApp(home)
	makeLegacyProject(t, a, home, proj, repo)
	storeBefore := digestDir(t, filepath.Join(home, "store"))

	if _, stderr, code := runGskillWithApp(t, a, proj, "project", "sync"); code != 0 {
		t.Fatalf("sync exit %d: %s", code, stderr)
	}

	assertRepoOwnedDemo(t, proj)
	lines := strings.Count(notice.String(), "\n")
	if lines != 1 || !strings.Contains(notice.String(), "migrated 1 skill(s) to repo-owned storage") {
		t.Errorf("notice = %q, want exactly one migration line", notice.String())
	}
	if got := digestDir(t, filepath.Join(home, "store")); got != storeBefore {
		t.Error("old store bytes changed during migration")
	}

	// A second run migrates nothing and stays quiet.
	notice.Reset()
	if _, stderr, code := runGskillWithApp(t, a, proj, "project", "sync"); code != 0 {
		t.Fatalf("second sync exit %d: %s", code, stderr)
	}
	if notice.Len() != 0 {
		t.Errorf("second run emitted a notice: %q", notice.String())
	}
}

// TestAutoMigrate_OfflineFromStoreObject: with the source gone and the clone
// cache emptied, the hash-valid old store object alone completes migration.
func TestAutoMigrate_OfflineFromStoreObject(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a, _ := noticeApp(home)
	makeLegacyProject(t, a, home, proj, repo)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(home, "cache")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskillWithApp(t, a, proj, "project", "sync"); code != 0 {
		t.Fatalf("offline migration exit %d, want 0 (store object is valid): %s", code, stderr)
	}
	assertRepoOwnedDemo(t, proj)
}

// TestAutoMigrate_ColdCacheFetchesByCommit: with the store object gone and a
// cold cache, migration re-fetches the recorded commit from the source.
func TestAutoMigrate_ColdCacheFetchesByCommit(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a, _ := noticeApp(home)
	hash := makeLegacyProject(t, a, home, proj, repo)
	if err := os.RemoveAll(filepath.Join(home, "store", "sha256", strings.TrimPrefix(hash, "sha256:"))); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(home, "cache")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskillWithApp(t, a, proj, "project", "sync"); code != 0 {
		t.Fatalf("cold-cache migration exit %d, want 0 (fetch by commit): %s", code, stderr)
	}
	assertRepoOwnedDemo(t, proj)
}

// TestAutoMigrate_FrozenConvertsLinksNotLock (spec 022 FR-013 carve-out): a
// frozen install on a legacy project converts links and state but leaves the
// lockfile byte-identical.
func TestAutoMigrate_FrozenConvertsLinksNotLock(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a, _ := noticeApp(home)
	makeLegacyProject(t, a, home, proj, repo)
	lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))

	if _, stderr, code := runGskillWithApp(t, a, proj, "install", "--frozen-lockfile", "--agent", "claude"); code != 0 {
		t.Fatalf("frozen install exit %d: %s", code, stderr)
	}

	if string(readFile(t, filepath.Join(proj, "skills-lock.json"))) != string(lockBefore) {
		t.Error("--frozen-lockfile rewrote the lockfile during migration")
	}
	entry := filepath.Join(proj, ".agents", "skills", "demo")
	if fi, err := os.Lstat(entry); err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		t.Errorf("active entry not converted under frozen install (err=%v)", err)
	}
}

// TestAutoMigrate_RemoveSkipsConversion (spec 022 FR-013 carve-out): a skill
// being removed is removed directly — no content conversion, no fetch, even
// with a cold cache and the source gone.
func TestAutoMigrate_RemoveSkipsConversion(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a, _ := noticeApp(home)
	hash := makeLegacyProject(t, a, home, proj, repo)
	for _, gone := range []string{
		repo, filepath.Join(home, "cache"),
		filepath.Join(home, "store", "sha256", strings.TrimPrefix(hash, "sha256:")),
	} {
		if err := os.RemoveAll(gone); err != nil {
			t.Fatal(err)
		}
	}

	if _, stderr, code := runGskillWithApp(t, a, proj, "remove", "demo", "--force"); code != 0 {
		t.Fatalf("remove exit %d, want 0 (no conversion for a removed skill): %s", code, stderr)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".agents", "skills", "demo")); !os.IsNotExist(err) {
		t.Error("legacy active entry not removed")
	}
}

// TestAutoMigrate_NonMutatingLeavesLegacyUntouched: list and check read a
// legacy project without modifying anything.
func TestAutoMigrate_NonMutatingLeavesLegacyUntouched(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a, notice := noticeApp(home)
	makeLegacyProject(t, a, home, proj, repo)
	before := digestDir(t, proj)

	for _, args := range [][]string{{"list"}, {"project", "check"}} {
		if _, stderr, code := runGskillWithApp(t, a, proj, args...); code != 0 {
			t.Fatalf("%v exit %d: %s", args, code, stderr)
		}
	}
	if got := digestDir(t, proj); got != before {
		t.Error("a non-mutating command modified a legacy project")
	}
	if notice.Len() != 0 {
		t.Errorf("non-mutating commands emitted a notice: %q", notice.String())
	}
}
