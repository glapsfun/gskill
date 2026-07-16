package globalstore

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/home"
)

// metadataSchemaVersion is the schema this build reads and writes. Readers
// reject records with a different version rather than misinterpreting them.
const metadataSchemaVersion = 1

// Metadata is the descriptive, mutable-under-lock record beside an object's
// immutable content. It never affects object identity.
type Metadata struct {
	SchemaVersion int       `json:"schemaVersion"`
	ContentHash   string    `json:"contentHash"`
	SizeBytes     int64     `json:"sizeBytes"`
	CreatedAt     time.Time `json:"createdAt"`
	LastUsedAt    time.Time `json:"lastUsedAt,omitzero"`
	Origins       []Origin  `json:"origins"`
}

// Origin records one known source of an object's content. Origins are
// descriptive only; identical content from different sources shares one
// object. Credentials and tokens are never recorded.
type Origin struct {
	SourceType string `json:"sourceType,omitempty"`
	Source     string `json:"source,omitempty"`
	SkillPath  string `json:"skillPath,omitempty"`
	Version    string `json:"version,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

// originKey is the identity under which origins de-duplicate.
func (o Origin) originKey() string {
	return strings.Join([]string{o.SourceType, o.Source, o.SkillPath, o.Version, o.Ref, o.Commit}, "\x00")
}

// MergeOrigins returns existing with extra merged in, sorted by source (then
// skill path, then commit) and de-duplicated. The inputs are not mutated.
func MergeOrigins(existing []Origin, extra ...Origin) []Origin {
	merged := slices.Clone(existing)
	for _, o := range extra {
		dup := slices.ContainsFunc(merged, func(m Origin) bool {
			return m.originKey() == o.originKey()
		})
		if !dup {
			merged = append(merged, o)
		}
	}
	slices.SortFunc(merged, func(a, b Origin) int {
		if c := strings.Compare(a.Source, b.Source); c != 0 {
			return c
		}
		if c := strings.Compare(a.SkillPath, b.SkillPath); c != 0 {
			return c
		}
		return strings.Compare(a.Commit, b.Commit)
	})
	return merged
}

// WriteMetadata writes meta atomically and deterministically with owner-only
// permissions.
func WriteMetadata(path string, meta Metadata) error {
	meta.Origins = MergeOrigins(meta.Origins) // normalize order
	return fsutil.WriteJSONAtomic(path, meta, home.FilePerm())
}

// ReadMetadata loads and validates a metadata record, rejecting unknown
// schema versions with a clear error (forward compatibility, Constitution II).
func ReadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is store-internal
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata: %w", err)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, fmt.Errorf("parse metadata %s: %w", path, err)
	}
	if meta.SchemaVersion != metadataSchemaVersion {
		return Metadata{}, fmt.Errorf(
			"metadata %s has schema version %d; this gskill understands version %d — upgrade gskill or repair the object",
			path, meta.SchemaVersion, metadataSchemaVersion)
	}
	return meta, nil
}
