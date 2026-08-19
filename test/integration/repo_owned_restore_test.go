package integration_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// appWithHomeAt builds an App bound to an explicit private home path.
func appWithHomeAt(home string) *app.App {
	return app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: home,
	})
}

// These tests pin spec 022's restore matrix (FR-005/FR-006/FR-007, US2):
// skill source repos are cached commit-keyed under the home; committed
// content that matches the lock needs no fetch at all; missing content
// restores from the clone cache; only a cold cache fetches — exactly once.

// commitOf returns the lock-recorded commit for a skill.
func commitOf(t *testing.T, proj, skill string) string {
	t.Helper()
	commit, _ := lockEntryGskill(t, proj, skill)["commit"].(string)
	if commit == "" {
		t.Fatalf("no commit recorded for %s", skill)
	}
	return commit
}

// TestRepoOwned_RecordsCarryNoStoreFields (spec 022 FR-003, data-model §3/§4):
// after `add`, the clone cache holds the commit, and neither the lock's
// gskill block nor state.json carries store-location fields or absolute
// paths.
func TestRepoOwned_RecordsCarryNoStoreFields(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a := appWithHomeAt(home)
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	// Commit-keyed clone in the home cache.
	commit := commitOf(t, proj, "demo")
	if _, err := os.Stat(filepath.Join(home, "cache", commit)); err != nil {
		t.Errorf("clone cache missing commit %s: %v", commit, err)
	}

	// No store fields in the lock's gskill block.
	ext := lockEntryGskill(t, proj, "demo")
	for _, dead := range []string{"storeHash", "scope"} {
		if _, ok := ext[dead]; ok {
			t.Errorf("lock gskill block still carries %q: %v", dead, ext[dead])
		}
	}

	// No store fields or absolute paths in state.json.
	state := readFile(t, filepath.Join(proj, ".gskill", "state.json"))
	for _, dead := range []string{"storeHash", "storeScope", "activeTarget", "activeMode"} {
		if strings.Contains(string(state), `"`+dead+`"`) {
			t.Errorf("state.json still carries %q:\n%s", dead, state)
		}
	}
	lock := readFile(t, filepath.Join(proj, "skills-lock.json"))
	for _, blob := range []struct{ name, data string }{{"lock", string(lock)}, {"state", string(state)}} {
		if strings.Contains(blob.data, home) {
			t.Errorf("%s references the gskill home:\n%s", blob.name, blob.data)
		}
	}
}

// TestRepoOwned_FrozenRestoreFromCommittedContent: a fresh clone with
// committed content installs with zero network and zero cache — the
// committed content matching the lock hash IS the restore (FR-007 fast
// path). Proven with a brand-new home and the source repo deleted.
func TestRepoOwned_FrozenRestoreFromCommittedContent(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	// "Fresh clone": commit the project and clone it (drops gitignored
	// .gskill/, preserves committed content + relative links).
	gitRun(t, proj, "init", "--quiet", "-b", "main")
	gitRun(t, proj, "add", ".")
	gitRun(t, proj, "commit", "--quiet", "-m", "project with committed skill")
	work := t.TempDir()
	gitRun(t, work, "clone", "--quiet", proj, "clone")
	clone := filepath.Join(work, "clone")

	// New machine (empty home), source gone: only the committed content can
	// satisfy the install.
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	a := newAppWithHome(t)
	if _, stderr, code := runGskillWithApp(t, a, clone, "install", "--frozen-lockfile", "--agent", "claude"); code != 0 {
		t.Fatalf("frozen install on fresh clone exit %d, want 0 (committed content is the restore): %s", code, stderr)
	}

	// Lock stays byte-identical under --frozen-lockfile.
	orig := readFile(t, filepath.Join(proj, "skills-lock.json"))
	after := readFile(t, filepath.Join(clone, "skills-lock.json"))
	if string(orig) != string(after) {
		t.Error("--frozen-lockfile mutated the lockfile")
	}
}

// TestRepoOwned_RestoreFromCloneCache: deleted content in a second project
// restores from the commit-keyed clone cache with the source repo gone — the
// no-refetch guardrail (SC-002) in its post-022 form.
func TestRepoOwned_RestoreFromCloneCache(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	home := filepath.Join(t.TempDir(), "gskill-home")
	a := appWithHomeAt(home)
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	// Second project: same committed lock, no content, no source, no store —
	// only the clone cache (shared home) can satisfy the restore.
	proj2 := newProject(t)
	lock := readFile(t, filepath.Join(proj, "skills-lock.json"))
	if err := os.WriteFile(filepath.Join(proj2, "skills-lock.json"), lock, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(home, "store")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskillWithApp(t, a, proj2, "install", "--agent", "claude"); code != 0 {
		t.Fatalf("install exit %d, want 0 (clone-cache restore, no network): %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(proj2, ".agents", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("restored content missing: %v", err)
	}
}

// TestRepoOwned_InstallOnDriftFailsWithHint (spec 022 FR-008, quickstart
// scenario 5): hand-edited committed content makes plain `install` fail with
// the drift error and repair hint; `install --force` restores lock-true
// content.
func TestRepoOwned_InstallOnDriftFailsWithHint(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	entry := filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")
	if err := os.WriteFile(entry, []byte("# hand-edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runGskill(t, proj, "install", "--agent", "claude")
	if code == 0 {
		t.Fatal("install on drifted committed content succeeded, want fail-closed")
	}
	if !strings.Contains(stderr, "no longer matches skills-lock.json") || !strings.Contains(stderr, "repair") {
		t.Errorf("missing drift error or repair hint, stderr:\n%s", stderr)
	}
	if got := readFile(t, entry); string(got) != "# hand-edited\n" {
		t.Errorf("plain install modified drifted content: %q", got)
	}
	if _, stderr, code := runGskill(t, proj, "install", "--agent", "claude", "--force"); code != 0 {
		t.Fatalf("install --force exit %d, want 0 (restore path): %s", code, stderr)
	}
	if got := readFile(t, entry); strings.Contains(string(got), "hand-edited") {
		t.Error("install --force did not restore lock-true content")
	}
}

// TestRepoOwned_ColdCacheFetchesFromSource: with no committed content and a
// cold cache, install fetches from the source (once) and succeeds.
func TestRepoOwned_ColdCacheFetchesFromSource(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	proj2 := newProject(t)
	lock := readFile(t, filepath.Join(proj, "skills-lock.json"))
	if err := os.WriteFile(filepath.Join(proj2, "skills-lock.json"), lock, 0o600); err != nil {
		t.Fatal(err)
	}

	a := newAppWithHome(t) // cold cache, empty store
	if _, stderr, code := runGskillWithApp(t, a, proj2, "install", "--agent", "claude"); code != 0 {
		t.Fatalf("cold-cache install exit %d, want 0 (single fetch from source): %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(proj2, ".agents", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("restored content missing: %v", err)
	}
}
