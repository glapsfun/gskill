// Package fsutil provides the filesystem primitives gskill relies on for safe,
// reproducible mutations: atomic file writes, same-filesystem staging temp
// dirs, and a symlink-or-copy activation that records the method actually used.
package fsutil

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	dirPerm fs.FileMode = 0o750
)

// WriteFileAtomic writes data to path atomically. It creates parent directories
// as needed, writes to a temp file on the same filesystem, fsyncs it, then
// renames over path so readers never observe a partial file.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if mkErr := os.MkdirAll(dir, dirPerm); mkErr != nil {
		return fmt.Errorf("create dir %s: %w", dir, mkErr)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we fail before the rename.
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

// TempDir creates a uniquely named temporary directory under parent (creating
// parent if needed), so staging happens on the same filesystem as the eventual
// destination and can be promoted with a rename.
func TempDir(parent, pattern string) (string, error) {
	if err := os.MkdirAll(parent, dirPerm); err != nil {
		return "", fmt.Errorf("create staging parent %s: %w", parent, err)
	}
	dir, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	return dir, nil
}

// SymlinkOrCopy activates src at dst, preferring a symlink and falling back to a
// recursive copy when the filesystem rejects symlinks. It reports whether a
// symlink was used (false means a copy was made), which the caller records as
// the actual install mode.
func SymlinkOrCopy(src, dst string) (symlinked bool, err error) {
	if err = os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return false, fmt.Errorf("create dst parent: %w", err)
	}
	if err = os.RemoveAll(dst); err != nil {
		return false, fmt.Errorf("clear dst %s: %w", dst, err)
	}

	abs, err := filepath.Abs(src)
	if err != nil {
		return false, fmt.Errorf("resolve src: %w", err)
	}
	if linkErr := os.Symlink(abs, dst); linkErr == nil {
		return true, nil
	}
	if copyErr := CopyDir(src, dst); copyErr != nil {
		return false, fmt.Errorf("copy fallback: %w", copyErr)
	}
	return false, nil
}

// CopyDir recursively copies the tree rooted at src into dst, preserving file
// permissions (including the exec bit) and recreating symlinks as symlinks
// without following them.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		switch {
		case d.IsDir():
			if err := os.MkdirAll(target, dirPerm); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
			return nil
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read link %s: %w", path, err)
			}
			if err := os.Symlink(link, target); err != nil {
				return fmt.Errorf("recreate link %s: %w", target, err)
			}
			return nil
		default:
			return copyFile(path, target, info.Mode().Perm())
		}
	})
}

// copyFile copies a single regular file, applying perm to the destination.
func copyFile(src, dst string, perm fs.FileMode) (err error) {
	in, err := os.Open(src) //nolint:gosec // staging path is caller-controlled
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) //nolint:gosec // staging path is caller-controlled
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", dst, cerr)
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s: %w", dst, err)
	}
	return nil
}

// WriteJSONAtomic marshals v as indented JSON with a trailing newline and
// writes it atomically to path. encoding/json emits struct fields in
// declaration order and sorts map keys, so writes are deterministic for a
// given value (Constitution I).
func WriteJSONAtomic(path string, v any, perm fs.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return WriteFileAtomic(path, append(data, '\n'), perm)
}

// OwnerOnlyTempDir creates a uniquely named staging directory under parent
// (creating parent if needed) with owner-only permissions, for content that
// must never be readable or writable by other users while in flight.
func OwnerOnlyTempDir(parent, pattern string) (string, error) {
	dir, err := TempDir(parent, pattern)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // directories need the owner exec bit
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("restrict temp dir: %w", err)
	}
	return dir, nil
}

// DirSize sums the sizes of regular files under dir.
func DirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// ListStaleDirs returns the direct subdirectories of parent whose
// modification time is older than maxAge — abandoned staging left behind by
// interrupted processes. A missing parent yields no entries.
func ListStaleDirs(parent string, maxAge time.Duration) ([]string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", parent, err)
	}
	cutoff := time.Now().Add(-maxAge)
	var stale []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // vanished mid-scan
		}
		if info.ModTime().Before(cutoff) {
			stale = append(stale, filepath.Join(parent, entry.Name()))
		}
	}
	return stale, nil
}
