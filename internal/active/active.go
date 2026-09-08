package active

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/integrity"
)

// Layout constants for the active layer.
const (
	rootDir   = ".agents"
	skillsDir = "skills"
	// tmpMarker/oldMarker prefix the transient siblings swapIn creates next to
	// an active entry. They are never skill names: List filters them so a
	// crash-orphaned staging directory is not mistaken for an installed skill
	// (and pruned/reported as one).
	tmpMarker = "..gskill-switch-"
	oldMarker = "..gskill-old-"
)

// Health classifies the state of an active entry in the repo-owned model
// (spec 022): the entry is a real committed directory, and its identity is
// its content hash against the lock.
type Health string

// Active-entry health states.
const (
	// HealthOK means the entry is a real directory whose content hash matches
	// the expected (lock-recorded) hash.
	HealthOK Health = "ok"
	// HealthMissing means no entry exists.
	HealthMissing Health = "missing"
	// HealthDrifted means the entry is a real directory whose content no
	// longer matches the expected hash (hand-edited committed content).
	HealthDrifted Health = "drifted"
	// HealthLegacy means the entry is a symlink into a known legacy store
	// root (the pre-022 layout); migration converts it on the next mutating
	// command.
	HealthLegacy Health = "legacy"
	// HealthForeign means something gskill does not own occupies the entry:
	// a symlink resolving elsewhere, or a plain file.
	HealthForeign Health = "foreign"
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

// EnsureOptions parameterizes EnsureActive.
type EnsureOptions struct {
	// ExpectedHash is the content hash the entry must match after the call
	// (the lock's recorded hash for the incoming content). Required.
	ExpectedHash string
	// AcceptHashes are additional gskill-owned content hashes (e.g. the
	// previously locked version): an existing directory matching one of them
	// is replaced rather than treated as foreign.
	AcceptHashes []string
	// Replace allows replacing a real directory that matches neither
	// ExpectedHash nor AcceptHashes. Reconcile paths (install/sync/repair,
	// --force) set it; guarded add paths leave it false so drifted or foreign
	// content fails closed.
	Replace bool
	// LegacyRoots are store roots from the pre-022 layout: a symlink entry
	// resolving under one of them is a stale managed link and is replaced by
	// the real copy. Symlinks resolving anywhere else are foreign.
	LegacyRoots []string
}

// EnsureActive makes .agents/skills/<name> a real directory whose content is
// copied from src and verified against opts.ExpectedHash (spec 022: the repo
// owns skill content; the entry is committed, never a link into a store). It
// is idempotent — an entry already matching ExpectedHash is left untouched —
// and atomic: the copy is staged as a temporary sibling, verified, then
// swapped in, so the project never observes a half-written skill. It NEVER
// destroys content gskill does not own: foreign symlinks and unrecognized
// directories fail closed and are left intact.
func EnsureActive(root, name, src string, opts EnsureOptions) (string, error) {
	if opts.ExpectedHash == "" {
		return "", fmt.Errorf("ensure active %s: expected content hash is required", name)
	}
	dest := Path(root, name)
	info, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return dest, swapIn(dest, src, opts.ExpectedHash, name)
		}
		return "", fmt.Errorf("stat active %s: %w", name, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return ensureOverLink(dest, src, name, opts)
	}
	if !info.IsDir() {
		return "", foreignErr(name, dest, "a plain file")
	}
	return ensureOverDir(dest, src, name, opts)
}

// ensureOverLink handles a symlink occupant: a link into a legacy store root
// is a stale managed entry from the pre-022 layout and is replaced with the
// real copy; anything else is foreign and fails closed.
func ensureOverLink(dest, src, name string, opts EnsureOptions) (string, error) {
	target, err := resolveLink(dest)
	if err != nil {
		return "", err
	}
	for _, legacyRoot := range opts.LegacyRoots {
		if underRoot(target, legacyRoot) {
			return dest, swapIn(dest, src, opts.ExpectedHash, name)
		}
	}
	return "", foreignErr(name, dest, target)
}

// ensureOverDir handles a real-directory occupant: idempotent when it already
// matches the expected content; replaced when it matches a previously owned
// hash (version change) or when the caller reconciles. A mismatch on a skill
// gskill previously installed (accept hashes were provided) is drift — the
// spec 022 FR-008 error with its repair hint; with no prior ownership claim
// the occupant is foreign.
func ensureOverDir(dest, src, name string, opts EnsureOptions) (string, error) {
	// One hash of dest answers every question below: VerifyDir hands back the
	// actual content hash, so the accept-hash comparison is a string compare
	// rather than another full recursive walk per candidate.
	ok, actual, err := integrity.VerifyDir(dest, opts.ExpectedHash)
	if err != nil {
		return "", fmt.Errorf("verify active %s: %w", name, err)
	}
	if ok {
		return dest, nil
	}
	hadPrior := false
	for _, h := range opts.AcceptHashes {
		if h == "" {
			continue
		}
		hadPrior = true
		if h == actual {
			return dest, swapIn(dest, src, opts.ExpectedHash, name)
		}
	}
	if opts.Replace {
		return dest, swapIn(dest, src, opts.ExpectedHash, name)
	}
	if hadPrior {
		return "", errs.WithHint(
			fmt.Errorf("%w: committed content for skill %q at %s no longer matches skills-lock.json",
				errs.ErrInvalidLock, name, Rel(name)),
			"run 'gskill project repair' (or 'gskill install --force') to restore lock-true content, or re-add the skill to adopt the edited content as a new version")
	}
	return "", foreignErr(name, dest, "directory content gskill did not install")
}

// swapIn stages a verified copy of src as a temporary sibling of dest, then
// swaps it in atomically: the previous occupant (link or directory) is moved
// aside, the staged copy renamed into place, and the old entry removed. On
// failure the previous occupant is restored.
func swapIn(dest, src, expectedHash, name string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("activate %s: %w", name, err)
	}
	stamp := time.Now().UnixNano()
	tmp := fmt.Sprintf("%s%s%d", dest, tmpMarker, stamp)
	if err := fsutil.CopyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("activate %s: %w", name, err)
	}
	ok, got, err := integrity.VerifyDir(tmp, expectedHash)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("activate %s: verify staged copy: %w", name, err)
	}
	if !ok {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("%w: staged content %s for %q does not match expected %s",
			errs.ErrIntegrity, got, name, expectedHash)
	}

	old := fmt.Sprintf("%s%s%d", dest, oldMarker, stamp)
	hadOld := false
	if _, statErr := os.Lstat(dest); statErr == nil {
		if err := os.Rename(dest, old); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("activate %s: move previous entry aside: %w", name, err)
		}
		hadOld = true
	}
	if err := os.Rename(tmp, dest); err != nil {
		if hadOld {
			_ = os.Rename(old, dest) // restore the previous occupant
		}
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("activate %s: %w", name, err)
	}
	if hadOld {
		_ = os.RemoveAll(old)
	}
	return nil
}

// foreignErr reports a non-gskill-managed occupant of an active entry.
func foreignErr(name, dest, target string) error {
	return fmt.Errorf("%w: active entry %s for skill %q is foreign (resolves to %s); remove it and retry",
		errs.ErrInvalidLock, dest, name, target)
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

// Owned reports whether dest is gskill-managed content: a symlink resolving
// under any of the given roots (the repo's .agents/skills root; agent links
// resolve there), or a real directory whose content hash matches one of
// acceptHashes (the active entry itself, or a copy-mode install). A missing
// dest is not owned. This is the single ownership predicate shared by the
// installer's overwrite guard and the plan layer's conflict detection, so the
// two cannot drift (spec 011 FR-016).
func Owned(dest string, roots []string, acceptHashes ...string) bool {
	info, err := os.Lstat(dest)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := resolveLink(dest)
		if err != nil {
			return false
		}
		for _, r := range roots {
			if underRoot(target, r) {
				return true
			}
		}
		return false
	}
	// Hash dest at most once: VerifyDir returns the actual hash, so every
	// further candidate is a string compare instead of another full walk
	// (the installer's overwrite guard passes up to three hashes per target).
	actual := ""
	for _, h := range acceptHashes {
		if h == "" {
			continue
		}
		if actual == "" {
			ok, got, err := integrity.VerifyDir(dest, h)
			if err != nil {
				return false
			}
			if ok {
				return true
			}
			actual = got
			continue
		}
		if h == actual {
			return true
		}
	}
	return false
}

// HealthOf reports the active entry's state against the expected content
// hash. legacyRoots name pre-022 store roots so their stale symlinks are
// classified as HealthLegacy (migratable) rather than HealthForeign.
func HealthOf(root, name, expectedHash string, legacyRoots ...string) (Health, error) {
	dest := Path(root, name)
	info, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return HealthMissing, nil
		}
		return "", fmt.Errorf("stat active %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := resolveLink(dest)
		if err != nil {
			return "", err
		}
		for _, legacyRoot := range legacyRoots {
			if underRoot(target, legacyRoot) {
				return HealthLegacy, nil
			}
		}
		return HealthForeign, nil
	}
	if !info.IsDir() {
		return HealthForeign, nil
	}
	ok, _, err := integrity.VerifyDir(dest, expectedHash)
	if err != nil {
		return "", fmt.Errorf("verify active %s: %w", name, err)
	}
	if !ok {
		return HealthDrifted, nil
	}
	return HealthOK, nil
}

// Remove deletes a gskill-managed active entry for name: a symlink (a legacy
// or stale managed link), or a real directory whose content matches one of
// acceptHashes (the lock-recorded content). It is a no-op when the entry is
// absent, and it never deletes content it cannot prove gskill installed — a
// drifted or foreign directory is left intact.
func Remove(root, name string, acceptHashes ...string) error {
	dest := Path(root, name)
	info, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat active %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(dest); err != nil {
			return fmt.Errorf("remove active %s: %w", name, err)
		}
		return nil
	}
	if !info.IsDir() {
		return nil // foreign plain file: never delete
	}
	if !anyHash(acceptHashes) {
		return nil // no ownership claim to check against: never delete
	}
	// dest is a real directory here (symlinks and plain files returned
	// above), so one hash serves every accept-hash comparison.
	h, err := integrity.HashDir(dest)
	if err != nil {
		return nil //nolint:nilerr // unverifiable content: never delete
	}
	for _, want := range acceptHashes {
		if want == "" || want != h.ContentHash {
			continue
		}
		if err := os.RemoveAll(dest); err != nil {
			return fmt.Errorf("remove active %s: %w", name, err)
		}
		return nil
	}
	return nil // unverifiable content: never delete
}

// anyHash reports whether hashes holds at least one non-empty entry.
func anyHash(hashes []string) bool {
	for _, h := range hashes {
		if h != "" {
			return true
		}
	}
	return false
}

// List returns the names of active entries (directories and symlinks) under
// root. Plain files are skipped: they are never gskill-managed.
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
		if strings.Contains(e.Name(), tmpMarker) || strings.Contains(e.Name(), oldMarker) {
			continue // transient swap sibling, not a skill
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat active entry %s: %w", e.Name(), err)
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
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
