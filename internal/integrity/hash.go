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
	// LegacyContentHash is the pre-028 identity of the same tree as read
	// from disk, where the owner execute bit tagged a file "x". It equals
	// ContentHash when no regular file reads back executable, and exists only
	// so lock entries written before spec 028 keep verifying.
	LegacyContentHash string
	// SkillFileHash is the hash of SKILL.md alone.
	SkillFileHash string
}

// Matches reports whether recorded is this tree's identity under the current
// rule or the pre-028 rule. An empty recorded value never matches.
func (h Hashes) Matches(recorded string) bool {
	return recorded != "" && (recorded == h.ContentHash || recorded == h.LegacyContentHash)
}

// fileEntry is one canonicalized file fed into the directory hash.
type fileEntry struct {
	path    string // relative, forward-slash
	symlink bool   // tagged "l"; every other entry is a regular file
	exec    bool   // owner execute bit as read back; feeds only the legacy hash
	data    []byte // LF-normalized text, verbatim binary, or link target
}

// HashContent returns the canonical hash of a single file's content, applying
// LF normalization to text. The result is prefixed with "sha256:".
func HashContent(data []byte) string {
	sum := sha256.Sum256(normalize(data))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashDir computes the canonical content hash of the skill directory and the
// separate SKILL.md hash. Paths are sorted, file modes are not part of the
// identity (spec 028: filesystems such as WSL DrvFs report every file as
// executable), text is LF-normalized, binaries are hashed verbatim, and
// content symlinks are never followed (FR-014, FR-042, D6). The pre-028
// mode-aware value is computed only when some file reads back executable.
func HashDir(dir string) (Hashes, error) {
	entries, skillData, err := collectEntries(dir)
	if err != nil {
		return Hashes{}, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	res := Hashes{ContentHash: hashEntries(entries, false)}
	res.LegacyContentHash = res.ContentHash
	for _, e := range entries {
		if e.exec {
			res.LegacyContentHash = hashEntries(entries, true)
			break
		}
	}
	if skillData != nil {
		res.SkillFileHash = HashContent(skillData)
	}
	return res, nil
}

// hashEntries feeds the sorted entries into one digest. legacy selects the
// pre-028 tag rule, under which an executable regular file is tagged "x".
func hashEntries(entries []fileEntry, legacy bool) string {
	h := sha256.New()
	for _, e := range entries {
		tag := "-"
		switch {
		case e.symlink:
			tag = "l"
		case legacy && e.exec:
			tag = "x"
		}
		_, _ = fmt.Fprintf(h, "%s\n%s\n%d\n", e.path, tag, len(e.data))
		_, _ = h.Write(e.data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
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
			entries = append(entries, fileEntry{path: rel, symlink: true, data: []byte(link)})
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

		if rel == SkillFileName {
			skillData = data
		}
		entries = append(entries, fileEntry{path: rel, exec: info.Mode()&0o100 != 0, data: data})
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
