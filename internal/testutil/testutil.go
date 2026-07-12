// Package testutil provides golden-file and temporary-directory helpers shared
// across gskill tests. It is imported only from _test.go files.
package testutil

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update reports whether golden files should be rewritten in place. Enable it
// with "go test -update".
var update = flag.Bool("update", false, "update golden files instead of comparing")

// Update reports whether the -update flag was set for this test run.
func Update() bool { return *update }

// Golden compares got against the golden file at testdata/<name>. When -update
// is set it rewrites the golden file instead of asserting equality, so golden
// files can be regenerated with "go test -update".
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("testutil: create golden dir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("testutil: write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path) //nolint:gosec // test-controlled golden path
	if err != nil {
		t.Fatalf("testutil: read golden %s: %v (run with -update to create it)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// TempProject returns an isolated temporary directory usable as a gskill
// project root for a single test. The directory is removed when the test ends.
func TempProject(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// GitEnv returns a subprocess environment safe for running git against a
// test's own isolated fixture repository: os.Environ() with every ambient
// GIT_* variable stripped, plus extra appended. Without this, a git command
// invoked from inside a real git hook (as gotest-short runs under
// pre-commit) inherits GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE that git sets for
// the hook subprocess tree, so a fixture's "git init" in its own t.TempDir()
// silently operates on the enclosing gskill repository instead — under
// t.Parallel() this races real git state and can corrupt it.
//
// This strips every GIT_* variable, unlike internal/git.sanitizedEnv's
// narrower repo-location-only allowlist: fixtures are local, no-auth repos
// that never need GIT_SSH_COMMAND/GIT_ASKPASS/GIT_CONFIG_*, so the blunter
// strip is safe here even though it would be wrong for gskill's own fetches.
func GitEnv(extra ...string) []string {
	env := os.Environ()
	filtered := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		filtered = append(filtered, kv)
	}
	return append(filtered, extra...)
}
