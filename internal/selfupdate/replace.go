package selfupdate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/glapsfun/gskill/internal/errs"
)

// File name prefixes for the replacement binary and the backup of the old
// one. Both live next to the executable only while an upgrade runs.
const (
	stagedPrefix = ".gskill-upgrade-"
	backupPrefix = ".gskill-previous-"
)

// File operations replaced in tests to force failures that a real
// filesystem cannot produce on demand.
var (
	link          = os.Link
	removeBackup  = os.Remove
	restoreRename = os.Rename
)

// Stage creates the file the new binary is written to, in dir and with like's
// permission bits, so the final rename stays on one filesystem.
func Stage(dir string, like os.FileInfo) (*os.File, error) {
	f, err := os.CreateTemp(dir, stagedPrefix+"*")
	if err != nil {
		return nil, errs.Wrap(errs.CodeGeneric, "create a staged file in "+dir, err)
	}
	if err := f.Chmod(like.Mode().Perm()); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, errs.Wrap(errs.CodeGeneric, "set the staged file's mode", err)
	}
	// A root-run upgrade of a user's binary keeps the user as its owner.
	if st, ok := like.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		_ = f.Chown(int(st.Uid), int(st.Gid))
	}
	return f, nil
}

// Install puts staged at exe and returns a backup of the old executable.
// It renames rather than copies: rename is the only atomic replacement, so exe
// is always one complete file, and on macOS arm64 writing into a signed binary
// in place gets its next run killed.
func Install(exe, staged string) (backup string, err error) {
	dir := filepath.Dir(exe)
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", errs.Wrap(errs.CodeGeneric, "name the backup file", err)
	}
	backup = filepath.Join(dir, backupPrefix+hex.EncodeToString(suffix))
	if err := link(exe, backup); err != nil {
		if err := copyFile(exe, backup); err != nil {
			return "", errs.Wrap(errs.CodeGeneric, "back up "+exe, err)
		}
	}
	if err := os.Rename(staged, exe); err != nil {
		_ = os.Remove(backup)
		return "", errs.Wrap(errs.CodeGeneric, "replace "+exe, err)
	}
	syncDir(dir)
	return backup, nil
}

// Restore moves the backup back over exe.
func Restore(exe, backup string) error {
	if err := restoreRename(backup, exe); err != nil {
		return errs.Wrap(errs.CodeGeneric, "restore the previous "+exe, err)
	}
	syncDir(filepath.Dir(exe))
	return nil
}

// Cleanup removes staged and backup files an interrupted upgrade left in dir.
// Callers hold the directory lock, so no other upgrade is using them.
func Cleanup(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errs.Wrap(errs.CodeGeneric, "read "+dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		isLeftover := (strings.HasPrefix(name, stagedPrefix) && len(name) > len(stagedPrefix)) ||
			(strings.HasPrefix(name, backupPrefix) && len(name) > len(backupPrefix))
		if isLeftover {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				return errs.Wrap(errs.CodeGeneric, "remove "+name, err)
			}
		}
	}
	return nil
}

// CheckWritable proves the executable's directory can take the replacement by
// creating and removing a staged file there. It is the authoritative check,
// run under the lock right before anything is downloaded.
func CheckWritable(exe string) error {
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, stagedPrefix+"*")
	if err != nil {
		return cannotWrite(exe, dir, err)
	}
	_ = f.Close()
	if err := os.Remove(f.Name()); err != nil {
		return errs.Wrap(errs.CodeGeneric, "remove "+f.Name(), err)
	}
	return nil
}

// cannotWrite explains why the executable's directory cannot take the new
// binary. Only a permission problem is fixed by running as another user, so
// only that gets the hint.
func cannotWrite(exe, dir string, cause error) error {
	switch {
	case errors.Is(cause, fs.ErrPermission):
		return permissionError(exe, dir)
	case errors.Is(cause, syscall.EROFS):
		return errs.WithHint(errs.New(errs.CodeGeneric, "cannot write "+exe+": read-only file system"),
			"the file system is read-only; reinstall into a writable directory with the install script")
	default:
		var pathErr *fs.PathError
		if errors.As(cause, &pathErr) {
			cause = pathErr.Err
		}
		return errs.Wrap(errs.CodeGeneric, "cannot write "+exe, cause)
	}
}

// Discard removes a backup once the new executable has checked out.
func Discard(backup string) error {
	if err := removeBackup(backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", backup, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src is the running executable
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()) //nolint:gosec // dst is a fresh name next to src
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("sync %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

// syncDir makes a rename durable. Failure only weakens durability, so it is
// not an error.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil { //nolint:gosec // the executable's own directory
		_ = d.Sync()
		_ = d.Close()
	}
}
