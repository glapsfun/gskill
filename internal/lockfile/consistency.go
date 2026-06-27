package lockfile

import (
	"fmt"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/manifest"
)

// CheckConsistency reports whether the manifest and lockfile agree closely
// enough that a frozen restore would not need to change the lock. A
// disagreement (a skill present in only one, or a changed source/requested
// version) maps to the lockfile-mismatch exit code 4 (FR-037, SC-002).
func CheckConsistency(m *manifest.Manifest, lf *Lockfile) error {
	for name, ms := range m.Skills {
		locked, ok := lf.Skills[name]
		if !ok {
			return fmt.Errorf("%w: skill %q is declared but not locked", errs.ErrLockMismatch, name)
		}
		if locked.Source.Original != ms.Source {
			return fmt.Errorf("%w: skill %q source changed (%q != %q)",
				errs.ErrLockMismatch, name, ms.Source, locked.Source.Original)
		}
		if locked.Requested.Version != ms.Version ||
			locked.Requested.Ref != ms.Ref ||
			locked.Requested.Commit != ms.Commit {
			return fmt.Errorf("%w: skill %q requested version changed since lock", errs.ErrLockMismatch, name)
		}
	}
	for name := range lf.Skills {
		if _, ok := m.Skills[name]; !ok {
			return fmt.Errorf("%w: skill %q is locked but no longer declared", errs.ErrLockMismatch, name)
		}
	}
	return nil
}
