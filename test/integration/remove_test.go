package integration_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// demoInstalledProject creates a fresh project with skill "demo" installed
// for its default agent — the shared baseline for the `remove` tests below.
func demoInstalledProject(t *testing.T) (proj string) {
	t.Helper()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj = newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	return proj
}

func TestRemove_ClearsManifestLockAgentDirsAndGCsStore(t *testing.T) {
	t.Parallel()

	proj := demoInstalledProject(t)

	if _, stderr, code := runGskill(t, proj, "remove", "demo", "--force"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}

	// Lockfile no longer locks it.
	if l := string(readFile(t, filepath.Join(proj, "skills-lock.json"))); strings.Contains(l, "demo") {
		t.Errorf("lockfile still references demo:\n%s", l)
	}
	// Agent dir is gone.
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("agent dir not removed (stat err=%v)", err)
	}
	// Store is garbage-collected (no content entries remain).
	if n := countFiles(t, filepath.Join(proj, ".gskill", "store")); n != 0 {
		t.Errorf("store still holds %d file(s) after GC", n)
	}
}

// TestRemove_NonInteractiveWithoutForce_AbortsWithoutChanges (spec 016
// FR-001-003, US1): the reported gap — an unattended `remove` used to
// silently delete with zero confirmation. It must now abort and leave the
// lockfile, agent dir, and store untouched.
func TestRemove_NonInteractiveWithoutForce_AbortsWithoutChanges(t *testing.T) {
	t.Parallel()

	proj := demoInstalledProject(t)
	lockBefore := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	storeFilesBefore := countFiles(t, filepath.Join(proj, ".gskill", "store"))

	_, stderr, code := runGskill(t, proj, "remove", "demo")
	if code == 0 {
		t.Fatalf("remove without --force: code = 0, want non-zero")
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("stderr = %q, want it to name --force", stderr)
	}

	if l := string(readFile(t, filepath.Join(proj, "skills-lock.json"))); l != lockBefore {
		t.Errorf("skills-lock.json changed:\nbefore: %s\nafter:  %s", lockBefore, l)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "demo")); err != nil {
		t.Errorf("agent dir removed: stat err=%v", err)
	}
	if n := countFiles(t, filepath.Join(proj, ".gskill", "store")); n != storeFilesBefore {
		t.Errorf("store file count changed: before=%d after=%d", storeFilesBefore, n)
	}
}

// TestRemove_MultiSkillForce_RemovesAllInOneInvocation (spec 016 US2
// Acceptance Scenario 2, SC-004): `--force` removes every named skill in a
// single unattended invocation, with the same full-removal semantics as the
// single-skill case.
func TestRemove_MultiSkillForce_RemovesAllInOneInvocation(t *testing.T) {
	t.Parallel()

	repoA := gitRepo(t, validSkill("demo"), "v1.0.0")
	repoB := gitRepo(t, validSkill("other"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repoA, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add demo: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repoB, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add other: %s", stderr)
	}

	if _, stderr, code := runGskill(t, proj, "remove", "demo", "other", "--force"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}

	if l := string(readFile(t, filepath.Join(proj, "skills-lock.json"))); strings.Contains(l, "demo") || strings.Contains(l, "other") {
		t.Errorf("lockfile still references demo or other:\n%s", l)
	}
	for _, skill := range []string{"demo", "other"} {
		if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", skill)); !os.IsNotExist(err) {
			t.Errorf("agent dir for %s not removed (stat err=%v)", skill, err)
		}
	}
	if n := countFiles(t, filepath.Join(proj, ".gskill", "store")); n != 0 {
		t.Errorf("store still holds %d file(s) after GC", n)
	}
}

// countFiles counts regular files under dir (absent dir counts as zero).
func countFiles(t *testing.T, dir string) int {
	t.Helper()

	count := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return count
}
