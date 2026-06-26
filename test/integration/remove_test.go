package integration_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemove_ClearsManifestLockAgentDirsAndGCsStore(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	if _, stderr, code := runGskill(t, proj, "remove", "demo"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}

	// Manifest no longer declares it.
	if m := string(readFile(t, filepath.Join(proj, "gskill.toml"))); strings.Contains(m, "demo") {
		t.Errorf("manifest still references demo:\n%s", m)
	}
	// Lockfile no longer locks it.
	if l := string(readFile(t, filepath.Join(proj, "gskill.lock"))); strings.Contains(l, "demo") {
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
