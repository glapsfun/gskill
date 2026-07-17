package globalstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/integrity"
)

func TestMetadata_RoundTripDeterministic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.json")
	meta := globalstore.Metadata{
		SchemaVersion: 1,
		ContentHash:   "sha256:abc",
		SizeBytes:     42,
		CreatedAt:     time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC),
		Origins: []globalstore.Origin{
			{SourceType: "github", Source: "github.com/example/skills", SkillPath: "skills/argocd", Version: "1.4.0", Ref: "v1.4.0", Commit: "aaa111"},
		},
	}
	if err := globalstore.WriteMetadata(path, meta); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	first, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if err := globalstore.WriteMetadata(path, meta); err != nil {
		t.Fatalf("second WriteMetadata: %v", err)
	}
	second, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("metadata writes are not deterministic")
	}

	got, err := globalstore.ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if got.ContentHash != meta.ContentHash || got.SizeBytes != meta.SizeBytes {
		t.Errorf("round trip = %+v, want %+v", got, meta)
	}
	if len(got.Origins) != 1 || got.Origins[0].Commit != "aaa111" {
		t.Errorf("origins round trip = %+v", got.Origins)
	}
}

func TestReadMetadata_RejectsUnknownSchemaVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion": 99, "contentHash": "sha256:abc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := globalstore.ReadMetadata(path)
	if err == nil {
		t.Fatal("ReadMetadata accepted schemaVersion 99")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error %q does not name the offending version", err)
	}
}

func TestMergeOrigins_SortedDeduplicated(t *testing.T) {
	t.Parallel()

	a := globalstore.Origin{SourceType: "github", Source: "github.com/org-b/skills", SkillPath: "skills/argocd", Commit: "bbb"}
	b := globalstore.Origin{SourceType: "github", Source: "github.com/org-a/skills", SkillPath: "skills/argocd", Commit: "aaa"}

	merged := globalstore.MergeOrigins([]globalstore.Origin{a}, b)
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want 2 entries", merged)
	}
	if merged[0].Source != "github.com/org-a/skills" {
		t.Errorf("merged not sorted by source: %+v", merged)
	}
	// Re-merging an existing origin is a no-op.
	again := globalstore.MergeOrigins(merged, a)
	if len(again) != 2 {
		t.Errorf("duplicate merge grew origins: %+v", again)
	}
}

// TestMetadataUpdates_NeverTouchContent asserts FR-004: origin merges and
// lastUsedAt touches leave the content directory byte-identical.
func TestMetadataUpdates_NeverTouchContent(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# immutable\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Source: "one", Commit: "c1"}); err != nil {
		t.Fatalf("Admit: %v", err)
	}

	before, err := integrity.HashDir(s.ContentPath(key))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RecordOrigin(t.Context(), key, globalstore.Origin{Source: "two", Commit: "c2"}); err != nil {
		t.Fatalf("RecordOrigin: %v", err)
	}
	if err := s.TouchLastUsed(t.Context(), key); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	after, err := integrity.HashDir(s.ContentPath(key))
	if err != nil {
		t.Fatal(err)
	}
	if before.ContentHash != after.ContentHash {
		t.Error("metadata updates modified content (FR-004 violation)")
	}

	obj, err := s.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(obj.Metadata.Origins) != 2 {
		t.Errorf("origins = %+v, want merged 2", obj.Metadata.Origins)
	}
	if obj.Metadata.LastUsedAt.IsZero() {
		t.Error("lastUsedAt not touched")
	}
}
