package lockfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/glapsfun/gskill/internal/fsutil"
)

// SchemaVersion is the lockfile schema this build understands (FR-006).
const SchemaVersion = 1

// ErrUnsupportedSchema is returned for a lockfile_version newer than this build.
var ErrUnsupportedSchema = errors.New("unsupported lockfile schema version")

// Lockfile is the machine-generated record of resolved reality (FR-003).
type Lockfile struct {
	LockfileVersion int                    `json:"lockfile_version"`
	Skills          map[string]LockedSkill `json:"skills"`
}

// LockedSkill is the full reproduction record for one skill.
type LockedSkill struct {
	Source       Source       `json:"source"`
	Requested    Requested    `json:"requested"`
	Resolved     Resolved     `json:"resolved"`
	Metadata     Metadata     `json:"metadata"`
	Requires     Requires     `json:"requires"`
	Installation Installation `json:"installation"`
	Provenance   Provenance   `json:"provenance"`
}

// Source is the normalized origin of a skill.
type Source struct {
	Type     string `json:"type"`
	Original string `json:"original"`
	URL      string `json:"url,omitempty"`
	Owner    string `json:"owner,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Path     string `json:"path,omitempty"`
}

// Requested echoes the human's intent (FR-010).
type Requested struct {
	Version string `json:"version,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// Resolved is the immutable identity gskill pinned to (FR-009, FR-010).
type Resolved struct {
	Version       string `json:"version,omitempty"`
	RefKind       string `json:"ref_kind"`
	Tag           string `json:"tag,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Commit        string `json:"commit,omitempty"`
	TreeHash      string `json:"tree_hash,omitempty"`
	ContentHash   string `json:"content_hash"`
	SkillFileHash string `json:"skill_file_hash,omitempty"`
	MutableRef    bool   `json:"mutable_ref"`
	LocalPathHash string `json:"local_path_hash,omitempty"`
}

// Metadata is the captured SKILL.md frontmatter.
type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version,omitempty"`
	License     string `json:"license,omitempty"`
}

// Requires records declared needs, surfaced but never resolved (FR-032).
type Requires struct {
	Skills      []string `json:"skills"`
	Commands    []string `json:"commands"`
	Environment []string `json:"environment"`
	MCP         []string `json:"mcp"`
}

// Installation is the placement record (FR-019, FR-020, FR-027, FR-028). Mode is
// the representative install mode; Modes records the actual mode per agent for
// the case where they differ (e.g. a symlink falls back to a copy).
type Installation struct {
	Scope   string            `json:"scope"`
	Mode    string            `json:"mode"`
	Agents  []string          `json:"agents"`
	Targets map[string]string `json:"targets"`
	Modes   map[string]string `json:"modes,omitempty"`
}

// Provenance is best-effort trust info. Timestamps are excluded from
// reproducible determinism (FR-004).
type Provenance struct {
	FetchedAt string `json:"fetched_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Trust     string `json:"trust"`
}

// New returns an empty lockfile at the current schema version.
func New() *Lockfile {
	return &Lockfile{LockfileVersion: SchemaVersion, Skills: make(map[string]LockedSkill)}
}

// Marshal serializes lf deterministically: sorted map keys, 2-space indent, a
// trailing newline, and no HTML escaping (FR-004).
func Marshal(lf *Lockfile) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(lf); err != nil {
		return nil, fmt.Errorf("marshal lockfile: %w", err)
	}
	return buf.Bytes(), nil
}

// Unmarshal parses lockfile bytes and refuses a newer schema version (FR-006).
func Unmarshal(data []byte) (*Lockfile, error) {
	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse lockfile: %w", err)
	}
	if lf.LockfileVersion > SchemaVersion {
		return nil, fmt.Errorf("%w: %d (this build understands up to %d; upgrade gskill)",
			ErrUnsupportedSchema, lf.LockfileVersion, SchemaVersion)
	}
	if lf.Skills == nil {
		lf.Skills = make(map[string]LockedSkill)
	}
	return &lf, nil
}

// Load reads and parses the lockfile at path.
func Load(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // project-root lockfile path
	if err != nil {
		return nil, fmt.Errorf("read lockfile %s: %w", path, err)
	}
	return Unmarshal(data)
}

// Save writes lf to path atomically.
func Save(path string, lf *Lockfile) error {
	data, err := Marshal(lf)
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write lockfile %s: %w", path, err)
	}
	return nil
}
