// Package testutil provides golden-file and temporary-directory helpers shared
// across gskill tests. It is imported only from _test.go files.
package testutil

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
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
