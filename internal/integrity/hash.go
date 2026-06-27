package integrity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// SkillFileName is the canonical skill manifest filename.
const SkillFileName = "SKILL.md"

// Hashes holds the canonical content identity of an installed skill (FR-014).
type Hashes struct {
	// ContentHash is the canonical recursive hash of the skill directory.
	ContentHash string
	// SkillFileHash is the hash of SKILL.md alone.
	SkillFileHash string
}

// fileEntry is one canonicalized file fed into the directory hash.
type fileEntry struct {
	path string // relative, forward-slash
	tag  string // "-" file, "x" executable, "l" symlink
	data []byte // LF-normalized text, verbatim binary, or link target
}

// HashContent returns the canonical hash of a single file's content, applying
// LF normalization to text. The result is prefixed with "sha256:".
func HashContent(data []byte) string {
	sum := sha256.Sum256(normalize(data))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashDir computes the canonical content hash of the skill directory and the
// separate SKILL.md hash. Paths are sorted, file modes are normalized to the
// executable bit, text is LF-normalized, binaries are hashed verbatim, and
// content symlinks are never followed (FR-014, FR-042, D6).
func HashDir(dir string) (Hashes, error) {
	entries, skillData, err := collectEntries(dir)
	if err != nil {
		return Hashes{}, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	h := sha256.New()
	for _, e := range entries {
		_, _ = fmt.Fprintf(h, "%s\n%s\n%d\n", e.path, e.tag, len(e.data))
		_, _ = h.Write(e.data)
	}

	res := Hashes{ContentHash: "sha256:" + hex.EncodeToString(h.Sum(nil))}
	if skillData != nil {
		res.SkillFileHash = HashContent(skillData)
	}
	return res, nil
}

// collectEntries walks dir, returning the canonicalized file entries and the raw
// SKILL.md bytes (nil if absent).
func collectEntries(dir string) (entries []fileEntry, skillData []byte, err error) {
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", p, err)
		}
		rel = filepath.ToSlash(rel)

		if d.Type()&fs.ModeSymlink != 0 {
			link, lErr := os.Readlink(p)
			if lErr != nil {
				return fmt.Errorf("read link %s: %w", p, lErr)
			}
			entries = append(entries, fileEntry{path: rel, tag: "l", data: []byte(link)})
			return nil
		}

		info, iErr := d.Info()
		if iErr != nil {
			return fmt.Errorf("stat %s: %w", p, iErr)
		}
		raw, rErr := os.ReadFile(p) //nolint:gosec // walking a caller-provided skill dir
		if rErr != nil {
			return fmt.Errorf("read %s: %w", p, rErr)
		}
		data := normalize(raw)

		tag := "-"
		if info.Mode()&0o100 != 0 {
			tag = "x"
		}
		if rel == SkillFileName {
			skillData = data
		}
		entries = append(entries, fileEntry{path: rel, tag: tag, data: data})
		return nil
	})
	if walkErr != nil {
		return nil, nil, fmt.Errorf("hash dir %s: %w", dir, walkErr)
	}
	return entries, skillData, nil
}

// normalize converts CRLF to LF for text, leaving binary content untouched.
func normalize(data []byte) []byte {
	if isBinary(data) {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

// isBinary reports whether data contains a NUL byte, the heuristic for binary.
func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}
