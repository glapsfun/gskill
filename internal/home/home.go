package home

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// EnvHome is the environment variable that relocates the entire gskill home.
// It is the only relocation mechanism; there is no config-file equivalent.
const EnvHome = "GSKILL_HOME"

// defaultDirName is the home directory name under the user's home on every
// platform (no XDG or macOS special-casing).
const defaultDirName = ".gskill"

// Owner-only permissions for everything under the home (FR-033).
const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// Dir resolves the gskill home directory: $GSKILL_HOME if set, else
// ~/.gskill. It does not create anything.
func Dir() (string, error) {
	if v := os.Getenv(EnvHome); v != "" {
		return v, nil
	}
	base, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(base, defaultDirName), nil
}

// Home is a resolved gskill home root with accessors for its fixed layout.
// Store, tmp, and locks all live under one root so staged content can be
// promoted with an atomic same-filesystem rename.
type Home struct {
	root string
}

// New returns a Home rooted at root.
func New(root string) *Home { return &Home{root: root} }

// Open resolves the home via Dir and ensures its layout exists.
func Open() (*Home, error) {
	root, err := Dir()
	if err != nil {
		return nil, err
	}
	h := New(root)
	if err := h.Ensure(); err != nil {
		return nil, err
	}
	return h, nil
}

// Root returns the home root directory.
func (h *Home) Root() string { return h.root }

// StoreDir returns the content-addressed global store root.
func (h *Home) StoreDir() string { return filepath.Join(h.root, "store") }

// CacheDir returns the download cache directory.
func (h *Home) CacheDir() string { return filepath.Join(h.root, "cache") }

// TmpDir returns the staging area for in-flight object admissions.
func (h *Home) TmpDir() string { return filepath.Join(h.root, "tmp") }

// LocksDir returns the directory holding all gskill lock files.
func (h *Home) LocksDir() string { return filepath.Join(h.root, "locks") }

// ProjectsDir returns the advisory project-registry directory.
func (h *Home) ProjectsDir() string { return filepath.Join(h.root, "projects") }

// PinsDir returns the directory of GC pin markers.
func (h *Home) PinsDir() string { return filepath.Join(h.root, "pins") }

// QuarantineDir returns where corrupted store objects are moved.
func (h *Home) QuarantineDir() string { return filepath.Join(h.root, "quarantine") }

// ConfigFile returns the user-level config file path inside the home.
func (h *Home) ConfigFile() string { return filepath.Join(h.root, "config.toml") }

// Ensure creates the home layout with owner-only permissions. It is
// idempotent and never loosens permissions on existing directories.
func (h *Home) Ensure() error {
	for _, dir := range []string{
		h.root, h.StoreDir(), h.CacheDir(), h.TmpDir(),
		h.LocksDir(), h.ProjectsDir(), h.PinsDir(), h.QuarantineDir(),
	} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create home dir %s: %w", dir, err)
		}
	}
	return nil
}

// Finding is one unsafe-permission observation under the home.
type Finding struct {
	// Path is the offending file or directory.
	Path string
	// Problem describes what is unsafe (e.g. world-writable).
	Problem string
	// Remedy is the suggested fix the user can run.
	Remedy string
}

// String renders the finding as a one-line diagnostic.
func (f Finding) String() string {
	return fmt.Sprintf("%s: %s (fix: %s)", f.Path, f.Problem, f.Remedy)
}

// CheckPerms audits the home tree for dangerous permissions: group- or
// world-writable directories/files and entries owned by another user. It
// reports findings rather than failing, so callers decide whether to warn or
// refuse (FR-033).
func (h *Home) CheckPerms() ([]Finding, error) {
	var findings []Finding
	err := filepath.WalkDir(h.root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // entry vanished mid-walk: skip, not fail
		}
		findings = append(findings, auditInfo(path, info)...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("audit home permissions: %w", err)
	}
	return findings, nil
}

// CheckPathSafety fails when path (a store object or other trusted location)
// is group/world-writable or owned by another user. Activation uses it to
// refuse unsafe objects fail-closed (FR-033).
func (h *Home) CheckPathSafety(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if fs := auditInfo(path, info); len(fs) > 0 {
		return fmt.Errorf("unsafe store path: %s", fs[0].String())
	}
	return nil
}

// auditInfo returns findings for one stat result.
func auditInfo(path string, info fs.FileInfo) []Finding {
	var findings []Finding
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		findings = append(findings, Finding{
			Path:    path,
			Problem: fmt.Sprintf("writable by other users (mode %o)", perm),
			Remedy:  "chmod go-w " + path,
		})
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if int(st.Uid) != os.Getuid() {
			findings = append(findings, Finding{
				Path:    path,
				Problem: fmt.Sprintf("owned by uid %d, not the current user", st.Uid),
				Remedy:  fmt.Sprintf("chown -R $(id -u) %s or remove it", path),
			})
		}
	}
	return findings
}

// FilePerm returns the owner-only file mode for files written under the home.
func FilePerm() fs.FileMode { return filePerm }
