package manifest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Validate applies the filesystem-dependent rules V5 and V6: every referenced
// path stays inside the repository and names a readable file. Both run before
// any network or cache access (FR-005), so a malformed manifest costs nothing.
func (m *Manifest) Validate(root string) error {
	if m == nil {
		return nil
	}
	names := make([]string, 0, len(m.Skills))
	for name := range m.Skills {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		s := m.Skills[name]
		if s.Override == nil {
			continue
		}
		if err := m.checkReplacePatchCollision(root, name, s.Override); err != nil {
			return err
		}
		for _, rel := range s.Override.Inputs() {
			abs, err := ResolveInRepo(root, rel)
			if err != nil {
				return usagef("%s: [skills.%s.override] %v", FileName, name, err)
			}
			info, statErr := os.Stat(abs)
			if statErr != nil {
				if errors.Is(statErr, fs.ErrNotExist) {
					return usagef("%s: [skills.%s.override] references %q, which does not exist", FileName, name, rel)
				}
				return usagef("%s: [skills.%s.override] cannot read %q: %v", FileName, name, rel, statErr)
			}
			if info.IsDir() {
				return usagef("%s: [skills.%s.override] references %q, which is a directory", FileName, name, rel)
			}
		}
	}
	return nil
}

// ResolveInRepo resolves a repo-relative path against root and proves the
// result stays inside the repository, after symlink resolution.
//
// Validation happens *after* resolution rather than by inspecting the string:
// only that defeats `..` segments, absolute paths, and symlinked escapes
// uniformly (research R2). Every committed artifact must stay in-repo (spec 022
// FR-003, extended by FR-006), so a path that escapes is refused outright.
func ResolveInRepo(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "~") {
		return "", errors.New("path " + rel + " must be repo-relative, not absolute")
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = filepath.Clean(root)
	}
	abs := filepath.Join(realRoot, filepath.FromSlash(rel))

	// Resolve what exists: EvalSymlinks fails on a missing leaf, so fall back
	// to the deepest existing ancestor. A symlinked *directory* pointing out of
	// the repo is caught this way even when the leaf itself is absent.
	probe := abs
	for {
		resolved, evalErr := filepath.EvalSymlinks(probe)
		if evalErr == nil {
			abs = filepath.Join(resolved, strings.TrimPrefix(abs, probe))
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}

	inside, err := filepath.Rel(realRoot, abs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", errors.New("path " + rel + " resolves outside the repository")
	}
	return abs, nil
}

// checkReplacePatchCollision implements V7. A file that is both replaced and
// patched has no defined result: the patch was authored against upstream, not
// against the replacement. That is user error, so gskill refuses rather than
// silently picking a winner.
func (m *Manifest) checkReplacePatchCollision(root, name string, o *Override) error {
	if len(o.Replace) == 0 || len(o.Patch) == 0 {
		return nil
	}
	replaced := map[string]bool{}
	for target := range o.Replace {
		replaced[filepath.ToSlash(filepath.Clean(target))] = true
	}
	for _, rel := range o.Patch {
		abs, err := ResolveInRepo(root, rel)
		if err != nil {
			return usagef("%s: [skills.%s.override] %v", FileName, name, err)
		}
		targets, err := patchTargets(abs)
		if err != nil {
			return usagef("%s: [skills.%s.override] cannot read patch %q: %v", FileName, name, rel, err)
		}
		for _, t := range targets {
			if replaced[t] {
				return usagef(
					"%s: [skills.%s.override] declares both a replace and a patch for %q; choose one",
					FileName, name, t)
			}
		}
	}
	return nil
}

// patchTargets extracts the files a unified diff writes to, by reading its
// `+++ b/<path>` headers. This is a header scan, not an applier: the
// authoritative per-hunk path check runs during application, where the diff is
// parsed properly and every resolved path is confined to the skill directory.
func patchTargets(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is already confined to the repo by ResolveInRepo
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "+++ ") {
			continue
		}
		t := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
		if i := strings.IndexAny(t, "\t"); i >= 0 {
			t = t[:i]
		}
		if t == "/dev/null" {
			continue
		}
		// Strip the conventional a/ or b/ prefix git writes.
		if len(t) > 2 && (strings.HasPrefix(t, "a/") || strings.HasPrefix(t, "b/")) {
			t = t[2:]
		}
		out = append(out, filepath.ToSlash(filepath.Clean(t)))
	}
	return out, nil
}
