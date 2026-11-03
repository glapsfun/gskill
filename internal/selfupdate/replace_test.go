package selfupdate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
)

func writeExe(t *testing.T, dir, body string, mode os.FileMode) string {
	t.Helper()
	exe := filepath.Join(dir, "gskill")
	if err := os.WriteFile(exe, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exe, mode); err != nil {
		t.Fatal(err)
	}
	return exe
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is inside the test's temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".gskill-upgrade-") || strings.HasPrefix(e.Name(), ".gskill-previous-") {
			t.Errorf("leftover %s in %s", e.Name(), dir)
		}
	}
}

// stageString stages body in dir like Apply does and returns the staged path.
func stageString(t *testing.T, dir string, like os.FileInfo, body string) string {
	t.Helper()
	staged, err := selfupdate.Stage(dir, like)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(staged.Name()), ".gskill-upgrade-") || filepath.Dir(staged.Name()) != dir {
		t.Errorf("staged at %s, want %s/.gskill-upgrade-*", staged.Name(), dir)
	}
	if _, err := staged.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	return staged.Name()
}

func TestStageAndInstall(t *testing.T) {
	t.Parallel()

	for _, mode := range []os.FileMode{0o755, 0o750} {
		dir := t.TempDir()
		exe := writeExe(t, dir, "old", mode)
		before, err := os.Stat(exe)
		if err != nil {
			t.Fatal(err)
		}

		backup, err := selfupdate.Install(exe, stageString(t, dir, before, "new"))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(exe)
		if err != nil {
			t.Fatal(err)
		}
		if got := readString(t, exe); got != "new" {
			t.Errorf("exe holds %q, want new", got)
		}
		if after.Mode().Perm() != mode {
			t.Errorf("mode = %v, want %v", after.Mode().Perm(), mode)
		}
		if os.SameFile(before, after) {
			t.Error("exe kept its inode; Install must rename a new file into place")
		}
		if got := readString(t, backup); got != "old" {
			t.Errorf("backup holds %q, want old", got)
		}

		if err := selfupdate.Discard(backup); err != nil {
			t.Fatal(err)
		}
		assertNoLeftovers(t, dir)
	}
}

func TestCleanup_RemovesOnlyUpgradeLeftovers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"gskill", "other", ".gskill-upgrade", ".gskill-upgrade-x", ".gskill-previous-y"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := selfupdate.Cleanup(dir); err != nil {
		t.Fatal(err)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if want := []string{".gskill-upgrade", "gskill", "other"}; strings.Join(left, ",") != strings.Join(want, ",") {
		t.Errorf("left %v, want %v", left, want)
	}
}

func TestRestore_PutsBackupBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	exe := writeExe(t, dir, "old", 0o755)
	before, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := selfupdate.Install(exe, stageString(t, dir, before, "new"))
	if err != nil {
		t.Fatal(err)
	}
	if err := selfupdate.Restore(exe, backup); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, exe); got != "old" {
		t.Errorf("exe holds %q after Restore, want old", got)
	}
	assertNoLeftovers(t, dir)
}

func TestCheckWritable(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can write any directory")
	}

	dir := t.TempDir()
	exe := writeExe(t, dir, "old", 0o755)
	if err := selfupdate.CheckWritable(exe); err != nil {
		t.Errorf("CheckWritable on a writable dir: %v", err)
	}
	assertNoLeftovers(t, dir)

	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // read-only directory under test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // restore so TempDir cleanup works
	err := selfupdate.CheckWritable(exe)
	if err == nil || err.Error() != "cannot write "+exe+": permission denied" || errs.ExitCode(err) != 1 {
		t.Errorf("CheckWritable on a read-only dir = %v, want cannot write with exit 1", err)
	}
	if want := "rerun as a user who can write " + dir + ", or reinstall into a writable directory with the install script"; errs.HintOf(err) != want {
		t.Errorf("hint = %q", errs.HintOf(err))
	}
}
