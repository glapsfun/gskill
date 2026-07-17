package globalstore_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
)

func TestVerifyObject_HealthyPasses(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# healthy\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyObject(key); err != nil {
		t.Errorf("VerifyObject on healthy object: %v", err)
	}
}

func TestVerifyObject_TamperFailsClosedAndQuarantines(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# will be tampered\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}

	// Tamper with the admitted content.
	victim := filepath.Join(s.ContentPath(key), "SKILL.md")
	if err := os.Chmod(filepath.Dir(victim), 0o700); err != nil { //nolint:gosec // intentional non-restrictive perms for the test
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := s.VerifyObject(key)
	if err == nil {
		t.Fatal("VerifyObject accepted tampered content")
	}
	if !errors.Is(err, errs.ErrIntegrity) {
		t.Errorf("err = %v, want ErrIntegrity", err)
	}
	if !strings.Contains(err.Error(), key) {
		t.Errorf("error %q does not carry the expected hash", err)
	}

	// The corrupted object is quarantined: gone from the store, present under
	// quarantine/.
	if s.Has(key) {
		t.Error("corrupted object still visible in the store")
	}
	entries, readErr := os.ReadDir(h.QuarantineDir())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("quarantine holds %d entries, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Name(), "sha256-") {
		t.Errorf("quarantine entry %q not derived from the object key", entries[0].Name())
	}
}

func TestVerifyObject_MissingObject(t *testing.T) {
	t.Parallel()

	s := globalstore.New(newTestHome(t))
	err := s.VerifyObject("sha256:absent")
	if !errors.Is(err, globalstore.ErrObjectNotFound) {
		t.Errorf("err = %v, want ErrObjectNotFound", err)
	}
}

// TestVerifyObject_SchemaVersionMismatchNeverQuarantines: an object written
// by a different gskill generation may be perfectly healthy — an older binary
// must refuse to use it, but never destroy shared content a newer binary can
// still serve.
func TestVerifyObject_SchemaVersionMismatchNeverQuarantines(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# future schema\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.MetadataPath(key))
	if err != nil {
		t.Fatal(err)
	}
	future := strings.Replace(string(raw), `"schemaVersion": 1`, `"schemaVersion": 99`, 1)
	if future == string(raw) {
		t.Fatalf("schemaVersion not found in metadata: %s", raw)
	}
	if err := os.WriteFile(s.MetadataPath(key), []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}

	err = s.VerifyObject(key)
	if !errors.Is(err, globalstore.ErrSchemaVersion) {
		t.Errorf("err = %v, want ErrSchemaVersion", err)
	}
	if !s.Has(key) {
		t.Error("newer-schema object was removed from the store")
	}
	if entries, readErr := os.ReadDir(h.QuarantineDir()); readErr == nil && len(entries) != 0 {
		t.Errorf("newer-schema object was quarantined: %v", entries)
	}
}

func TestVerifyObject_InvalidMetadataFails(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# meta breaks\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.MetadataPath(key), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyObject(key); err == nil {
		t.Error("VerifyObject accepted invalid metadata")
	}
}
