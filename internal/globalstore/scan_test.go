package globalstore_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/home"
)

// TestVerifyStore_Findings builds a store containing a healthy object, a
// tampered object, a malformed layout, invalid metadata, and a stray staging
// dir, and asserts the scan classifies each (FR-022).
func TestVerifyStore_Findings(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	keyBad, keyMeta := buildScanFixture(t, h, s)

	rep, err := s.VerifyStore(globalstore.ScanOptions{
		UsedBy: func(key string) []string {
			if key == keyBad {
				return []string{"/dev/repo1", "/dev/repo2"}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("VerifyStore: %v", err)
	}

	if rep.Checked != 4 {
		t.Errorf("Checked = %d, want 4", rep.Checked)
	}
	if rep.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1", rep.Healthy)
	}
	assertScanFindings(t, rep, keyBad, keyMeta)
	// The scan reports; it does not quarantine or delete.
	if !s.Has(keyBad) {
		t.Error("scan removed the corrupted object; reporting only")
	}
}

// buildScanFixture populates the store with a healthy object, a tampered
// object, a malformed layout, invalid metadata, and a stray staging dir.
func buildScanFixture(t *testing.T, h *home.Home, s *globalstore.Store) (keyBad, keyMeta string) {
	t.Helper()
	srcOK, keyOK := writeSkillDir(t, "# healthy\n")
	if _, err := s.Admit(t.Context(), keyOK, srcOK, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}
	srcBad, kb := writeSkillDir(t, "# will tamper\n")
	if _, err := s.Admit(t.Context(), kb, srcBad, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.ContentPath(kb), "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.StoreDir(), "sha256", "feedfacefeedface"), 0o700); err != nil {
		t.Fatal(err)
	}
	srcMeta, km := writeSkillDir(t, "# meta breaks\n")
	if _, err := s.Admit(t.Context(), km, srcMeta, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.MetadataPath(km), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(h.TmpDir(), "object-dead-1")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stray, past, past); err != nil {
		t.Fatal(err)
	}
	return kb, km
}

// assertScanFindings checks the report classifies each seeded problem.
func assertScanFindings(t *testing.T, rep globalstore.ScanReport, keyBad, keyMeta string) {
	t.Helper()
	byKind := map[globalstore.FindingKind][]globalstore.ScanFinding{}
	for _, f := range rep.Findings {
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}
	if got := byKind[globalstore.FindingCorrupted]; len(got) != 1 || got[0].Key != keyBad {
		t.Errorf("corrupted findings = %+v, want %s", got, keyBad)
	} else {
		if got[0].Expected != keyBad || got[0].Actual == "" || got[0].Actual == keyBad {
			t.Errorf("corrupted finding hashes: expected=%q actual=%q", got[0].Expected, got[0].Actual)
		}
		if len(got[0].UsedBy) != 2 {
			t.Errorf("corrupted finding UsedBy = %v, want two projects", got[0].UsedBy)
		}
	}
	if got := byKind[globalstore.FindingMalformed]; len(got) != 1 {
		t.Errorf("malformed findings = %+v, want 1", got)
	}
	if got := byKind[globalstore.FindingInvalidMetadata]; len(got) != 1 || got[0].Key != keyMeta {
		t.Errorf("invalid-metadata findings = %+v, want %s", got, keyMeta)
	}
	if got := byKind[globalstore.FindingStrayStaging]; len(got) != 1 {
		t.Errorf("stray-staging findings = %+v, want 1", got)
	}
}

// TestRepair_RestoresExactOrigin (FR-023): repair re-fetches the recorded
// commit, verifies the expected key, and atomically replaces the object; a
// fetch producing different content fails and replaces nothing.
func TestRepair_RestoresExactOrigin(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# original\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{
		SourceType: "local", Source: src, Commit: "aaa111",
	}); err != nil {
		t.Fatal(err)
	}
	// Tamper.
	if err := os.WriteFile(filepath.Join(s.ContentPath(key), "SKILL.md"), []byte("# bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Fetcher that reproduces the exact original content.
	fetch := func(_, commit, dest string) error {
		if commit != "aaa111" {
			t.Errorf("repair fetched commit %q, want recorded aaa111", commit)
		}
		return copyTree(src, dest)
	}
	if err := s.Repair(t.Context(), key, fetch); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if err := s.VerifyObject(key); err != nil {
		t.Errorf("object still bad after repair: %v", err)
	}
}

func TestRepair_WrongContentFailsWithoutReplacing(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# original two\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Source: src, Commit: "bbb"}); err != nil {
		t.Fatal(err)
	}
	tamperedBody := []byte("# bad\n")
	if err := os.WriteFile(filepath.Join(s.ContentPath(key), "SKILL.md"), tamperedBody, 0o600); err != nil {
		t.Fatal(err)
	}

	wrongSrc, _ := writeSkillDir(t, "# different content entirely\n")
	fetch := func(_, _, dest string) error { return copyTree(wrongSrc, dest) }
	if err := s.Repair(t.Context(), key, fetch); err == nil {
		t.Fatal("Repair accepted content with the wrong hash")
	}
	// Object untouched (still the tampered bytes — never silently replaced).
	data, err := os.ReadFile(filepath.Join(s.ContentPath(key), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(tamperedBody) {
		t.Error("failed repair modified the object")
	}
}

func TestRepair_NoCommitOriginFails(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# no commit origin\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Source: "somewhere"}); err != nil {
		t.Fatal(err)
	}
	fetch := func(string, string, string) error { return nil }
	if err := s.Repair(t.Context(), key, fetch); err == nil {
		t.Fatal("Repair without a commit-bearing origin should fail")
	}
}

// copyTree copies srcDir into dest for the fetch stubs.
func copyTree(srcDir, dest string) error {
	return os.CopyFS(dest, os.DirFS(srcDir))
}
