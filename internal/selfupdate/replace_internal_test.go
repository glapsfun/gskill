package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

//nolint:paralleltest // replaces the package-level link seam
func TestInstall_CopiesWhenHardLinksFail(t *testing.T) {
	link = func(string, string) error { return errors.New("cross-device link") }
	t.Cleanup(func() { link = os.Link })

	dir := t.TempDir()
	exe := filepath.Join(dir, "gskill")
	if err := os.WriteFile(exe, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, stagedPrefix+"x")
	if err := os.WriteFile(staged, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := Install(exe, staged)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(backup); string(got) != "old" { //nolint:gosec // backup path inside the test temp dir
		t.Errorf("copied backup holds %q, want old", got)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" { //nolint:gosec // exe path inside the test temp dir
		t.Errorf("exe holds %q, want new", got)
	}
}
