package active

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/integrity"
)

// Layout constants for the active layer.
const (
	rootDir   = ".agents"
	skillsDir = "skills"
)

// Health classifies the state of an active entry relative to its expected store
// target.
type Health string

// Active-entry health states.
const (
	// HealthOK means the entry is a symlink resolving to the expected store path.
	HealthOK Health = "ok"
	// HealthMissing means no entry exists.
	HealthMissing Health = "missing"
	// HealthBroken means the entry is a symlink whose target does not exist.
	HealthBroken Health = "broken"
	// HealthForeign means a non-symlink path occupies the entry (not gskill-managed).
	HealthForeign Health = "foreign"
	// HealthWrongStore means the entry is a symlink into the store but at the
	// wrong content path (e.g. stale after a content update).
	HealthWrongStore Health = "wrong-store-target"
)

// Dir returns the active-skills container directory under root.
func Dir(root string) string {
	return filepath.Join(root, rootDir, skillsDir)
}

// Path returns the active entry path for a skill name under root.
func Path(root, name string) string {
	return filepath.Join(Dir(root), name)
}

// Rel returns the project-relative active entry path for a skill name.
func Rel(name string) string {
	return filepath.Join(rootDir, skillsDir, name)
}

// EnsureActive makes the active entry for name resolve to storePath, preferring
// a symlink and falling back to a copy where symlinks are unsupported. It is
// idempotent (an entry already linking to storePath is left untouched) and
// re-points a stale gskill-managed symlink (one resolving into storeRoot) after a
// content update. It NEVER destroys foreign content: a symlink resolving outside
// storeRoot, or a real directory whose content does not match the store, fails
// closed and is left intact (FR-029/FR-030).
func EnsureActive(root, name, storePath, storeRoot string) (string, error) {
	dest := Path(root, name)
	info, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return create(dest, storePath, name)
		}
		return "", fmt.Errorf("stat active %s: %w", name, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, rErr := resolveLink(dest)
		if rErr != nil {
			return "", rErr
		}
		want, aErr := filepath.Abs(storePath)
		if aErr != nil {
			return "", fmt.Errorf("resolve store path: %w", aErr)
		}
		switch {
		case filepath.Clean(target) == filepath.Clean(want):
			return dest, nil // idempotent
		case underRoot(target, storeRoot):
			return create(dest, storePath, name) // stale managed symlink → re-point
		default:
			return "", foreignErr(name, dest, target)
		}
	}

	// A real directory: managed copy (or identical content) iff its content
	// matches the store; otherwise foreign — fail closed.
	expected, hErr := integrity.HashDir(storePath)
	if hErr != nil {
		return "", fmt.Errorf("hash store for %s: %w", name, hErr)
	}
	ok, _, vErr := integrity.VerifyDir(dest, expected.ContentHash)
	if vErr != nil {
		return "", fmt.Errorf("verify active %s: %w", name, vErr)
	}
	if ok {
		return dest, nil
	}
	return "", foreignErr(name, dest, "non-symlink content")
}

// create materializes the active entry as a symlink into storePath, copying where
// symlinks are unsupported.
func create(dest, storePath, name string) (string, error) {
	if _, err := fsutil.SymlinkOrCopy(storePath, dest); err != nil {
		return "", fmt.Errorf("activate %s: %w", name, err)
	}
	return dest, nil
}

// foreignErr reports a non-gskill-managed occupant of an active entry.
func foreignErr(name, dest, target string) error {
	return fmt.Errorf("%w: active entry %s for skill %q is foreign (resolves to %s); remove it and retry",
		errs.ErrInvalidManifest, dest, name, target)
}

// underRoot reports whether path is root or lives beneath it.
func underRoot(path, root string) bool {
	if root == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path = filepath.Clean(path)
	absRoot = filepath.Clean(absRoot)
	return path == absRoot || strings.HasPrefix(path, absRoot+string(filepath.Separator))
}

// HealthOf reports the active entry's state relative to the expected storePath.
func HealthOf(root, name, storePath string) (Health, error) {
	dest := Path(root, name)
	info, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return HealthMissing, nil
		}
		return "", fmt.Errorf("stat active %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return HealthForeign, nil
	}
	target, err := resolveLink(dest)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return HealthBroken, nil
		}
		return "", fmt.Errorf("stat active target %s: %w", name, err)
	}
	want, err := filepath.Abs(storePath)
	if err != nil {
		return "", fmt.Errorf("resolve store path: %w", err)
	}
	if filepath.Clean(target) != filepath.Clean(want) {
		return HealthWrongStore, nil
	}
	return HealthOK, nil
}

// Remove deletes a gskill-managed active entry (a symlink) for name. It is a
// no-op when the entry is absent, and it never deletes a non-symlink path, so a
// foreign directory occupying the name is left intact.
func Remove(root, name string) error {
	dest := Path(root, name)
	info, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat active %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil // foreign: never delete
	}
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("remove active %s: %w", name, err)
	}
	return nil
}

// List returns the names of gskill-managed active entries (symlinks) under root.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(Dir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read active dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat active entry %s: %w", e.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// resolveLink reads a symlink and returns its target as an absolute, cleaned
// path (resolving a relative link against the link's own directory).
func resolveLink(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", fmt.Errorf("read link %s: %w", path, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), nil
}
