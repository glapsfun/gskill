package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// CompatHash computes the skills-lock.json "computedHash" for a skill
// directory using the same rules as the external lock producer (the
// vercel-labs/skills CLI, src/local-lock.ts computeSkillFolderHash):
//
//   - collect regular files recursively; directories named ".git" or
//     "node_modules" are skipped at any depth
//   - symlinks are neither followed nor hashed; empty directories contribute
//     nothing; file modes are ignored
//   - file bytes are hashed verbatim (no line-ending normalization)
//   - files sort by relative forward-slash path using locale-aware collation
//     (JavaScript localeCompare)
//   - sha256 over the concatenation of each file's path then content, hex
//     encoded with no prefix
//
// Parity is pinned by TestCompatHashParity against hashes recorded from the
// reference implementation (testdata/compat). This is deliberately distinct
// from HashDir, gskill's own canonical hash, which stays in the namespaced
// gskill.storeHash field.
func CompatHash(dir string) (string, error) {
	files, err := collectCompatFiles(dir, dir)
	if err != nil {
		return "", err
	}

	coll := collate.New(language.Und)
	sort.SliceStable(files, func(i, j int) bool {
		return coll.CompareString(files[i].rel, files[j].rel) < 0
	})

	h := sha256.New()
	for _, f := range files {
		_, _ = h.Write([]byte(f.rel))
		_, _ = h.Write(f.data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// compatFile is one file fed into the compat hash.
type compatFile struct {
	rel  string // relative, forward-slash
	data []byte // verbatim bytes
}

// collectCompatFiles walks dir applying the reference implementation's
// collection rules.
func collectCompatFiles(base, dir string) ([]compatFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var out []compatFile
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir():
			if e.Name() == ".git" || e.Name() == "node_modules" {
				continue
			}
			sub, err := collectCompatFiles(base, full)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		case e.Type().IsRegular():
			data, err := os.ReadFile(full) //nolint:gosec // walking a caller-provided skill dir
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", full, err)
			}
			rel, err := filepath.Rel(base, full)
			if err != nil {
				return nil, fmt.Errorf("relativize %s: %w", full, err)
			}
			out = append(out, compatFile{rel: filepath.ToSlash(rel), data: data})
		default:
			// Symlinks and other non-regular entries are skipped, matching the
			// reference implementation's entry.isFile() check.
		}
	}
	return out, nil
}
