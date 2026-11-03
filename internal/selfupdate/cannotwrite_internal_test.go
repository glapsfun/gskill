package selfupdate

import (
	"io/fs"
	"syscall"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
)

// #5: only a permission problem gets the "rerun as another user" hint.
func TestCannotWrite(t *testing.T) {
	t.Parallel()

	const exe, dir = "/opt/x/gskill", "/opt/x"
	tests := []struct {
		name  string
		cause error
		msg   string
		hint  string
	}{
		{
			"permission", &fs.PathError{Op: "open", Path: dir, Err: syscall.EACCES}, "cannot write " + exe + ": permission denied",
			"rerun as a user who can write " + dir + ", or reinstall into a writable directory with the install script",
		},
		{
			"read-only filesystem", &fs.PathError{Op: "open", Path: dir, Err: syscall.EROFS}, "cannot write " + exe + ": read-only file system",
			"the file system is read-only; reinstall into a writable directory with the install script",
		},
		{"disk full", &fs.PathError{Op: "open", Path: dir, Err: syscall.ENOSPC}, "cannot write " + exe + ": no space left on device", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := cannotWrite(exe, dir, tt.cause)
			if err.Error() != tt.msg || errs.HintOf(err) != tt.hint || errs.ExitCode(err) != 1 {
				t.Errorf("cannotWrite = %q (hint %q, exit %d), want %q (hint %q, exit 1)",
					err, errs.HintOf(err), errs.ExitCode(err), tt.msg, tt.hint)
			}
		})
	}
}
