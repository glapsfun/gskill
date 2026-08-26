package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// OverrideSpec is a resolved override declaration, expressed with repo-relative
// paths. It mirrors the manifest and lockfile shapes without importing either,
// so the digest stays a leaf computation.
type OverrideSpec struct {
	Replace map[string]string
	Patch   []string
	Prepend []string
	Append  []string
}

// Empty reports whether the spec transforms nothing.
func (s OverrideSpec) Empty() bool {
	return len(s.Replace) == 0 && len(s.Patch) == 0 && len(s.Prepend) == 0 && len(s.Append) == 0
}

// OverrideDigest computes the canonical identity of an override declaration:
// SHA-256 over the declaration itself plus the content hash of every file it
// references (spec 023 FR-007, research R5).
//
// This is the Nix idea the feature rests on — output identity is a function of
// input identity. Including the referenced files' bytes is what makes drift
// detection mechanical (FR-010): editing house-rules.md changes an input hash,
// so the digest changes, with no file watching and no special cases.
//
// Determinism rules (Constitution I): kinds are visited in fixed pipeline
// order; declared list order is preserved because patches and layers apply in
// sequence, so that order is data; the Replace map is visited in sorted key
// order, because map iteration order is ambient environment and must never
// reach output. An empty declaration has no identity and digests to "".
func OverrideDigest(root string, spec OverrideSpec) (string, error) {
	if spec.Empty() {
		return "", nil
	}

	h := sha256.New()
	writeField := func(kind, declared, path string) error {
		sum, err := hashInputFile(root, path)
		if err != nil {
			return err
		}
		// Length-prefixing every component keeps the encoding unambiguous, so
		// no combination of paths can collide by concatenation.
		_, _ = fmt.Fprintf(h, "%s\n%d:%s\n%d:%s\n%s\n", kind, len(declared), declared, len(path), path, sum)
		return nil
	}

	targets := make([]string, 0, len(spec.Replace))
	for t := range spec.Replace {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, t := range targets {
		if err := writeField("replace", t, spec.Replace[t]); err != nil {
			return "", err
		}
	}

	for _, kind := range []struct {
		name string
		list []string
	}{
		{"patch", spec.Patch},
		{"prepend", spec.Prepend},
		{"append", spec.Append},
	} {
		for i, p := range kind.list {
			if err := writeField(kind.name, strconv.Itoa(i), p); err != nil {
				return "", err
			}
		}
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// hashInputFile hashes one referenced override input. A missing or unreadable
// input is an error rather than an empty contribution: digesting a declaration
// gskill could not fully read would assert an identity it cannot verify.
func hashInputFile(root, rel string) (string, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(p) //nolint:gosec // callers confine rel to the repo before digesting
	if err != nil {
		return "", fmt.Errorf("read override input %s: %w", rel, err)
	}
	return HashContent(data), nil
}
