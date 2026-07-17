package projreg_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/home"
	"github.com/glapsfun/gskill/internal/projreg"
)

func testHome(t *testing.T) *home.Home {
	t.Helper()
	h := home.New(filepath.Join(t.TempDir(), "gskill-home"))
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	return h
}

func sampleEntry(root string) projreg.Entry {
	return projreg.Entry{
		ProjectID: "p-abc123",
		Root:      root,
		Lockfile:  filepath.Join(root, "skills-lock.json"),
		LastSeen:  time.Now().UTC().Truncate(time.Second),
		References: []projreg.Reference{
			{Skill: "zeta", StoreHash: "sha256:bbb"},
			{Skill: "alpha", StoreHash: "sha256:aaa"},
		},
	}
}

func TestWrite_FullModeRoundTripOwnerOnly(t *testing.T) {
	t.Parallel()

	h := testHome(t)
	root := t.TempDir()
	if err := projreg.Write(h, sampleEntry(root), config.PrivacyFull); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, ok, err := projreg.Get(h, "p-abc123")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if got.Root != root {
		t.Errorf("Root = %q, want %q", got.Root, root)
	}
	if len(got.References) != 2 || got.References[0].Skill != "alpha" {
		t.Errorf("References = %+v, want sorted by skill", got.References)
	}

	files, err := os.ReadDir(filepath.Join(h.Root(), "projects"))
	if err != nil || len(files) != 1 {
		t.Fatalf("registry files: %v %v", files, err)
	}
	info, err := files[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("registry entry perm = %o, want 0600 (FR-029)", perm)
	}
}

func TestWrite_MinimalModeOmitsPaths(t *testing.T) {
	t.Parallel()

	h := testHome(t)
	root := t.TempDir()
	if err := projreg.Write(h, sampleEntry(root), config.PrivacyMinimal); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := projreg.Get(h, "p-abc123")
	if !ok {
		t.Fatal("entry missing")
	}
	if got.Root != "" || got.Lockfile != "" {
		t.Errorf("minimal mode recorded paths: root=%q lockfile=%q", got.Root, got.Lockfile)
	}
	if len(got.References) != 2 {
		t.Errorf("minimal mode dropped references: %+v", got.References)
	}
	raw, err := os.ReadFile(filepath.Join(h.Root(), "projects", "p-abc123.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), root) {
		t.Errorf("minimal entry leaks the project path:\n%s", raw)
	}
}

func TestWrite_DisabledModeRemovesEntry(t *testing.T) {
	t.Parallel()

	h := testHome(t)
	root := t.TempDir()
	if err := projreg.Write(h, sampleEntry(root), config.PrivacyFull); err != nil {
		t.Fatal(err)
	}
	if err := projreg.Write(h, sampleEntry(root), config.PrivacyDisabled); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := projreg.Get(h, "p-abc123"); ok {
		t.Error("disabled mode left the entry in place")
	}
}

func TestPrune_RemovesStaleKeepsLiveAndMinimal(t *testing.T) {
	t.Parallel()

	h := testHome(t)

	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(live, "skills-lock.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	liveEntry := sampleEntry(live)
	liveEntry.ProjectID = "p-live"
	if err := projreg.Write(h, liveEntry, config.PrivacyFull); err != nil {
		t.Fatal(err)
	}

	gone := sampleEntry(filepath.Join(t.TempDir(), "deleted-project"))
	gone.ProjectID = "p-gone"
	if err := projreg.Write(h, gone, config.PrivacyFull); err != nil {
		t.Fatal(err)
	}

	minimal := sampleEntry("")
	minimal.ProjectID = "p-minimal"
	minimal.Lockfile = ""
	if err := projreg.Write(h, minimal, config.PrivacyMinimal); err != nil {
		t.Fatal(err)
	}

	removed, err := projreg.Prune(h)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 1 || removed[0] != "p-gone" {
		t.Errorf("removed = %v, want [p-gone]", removed)
	}
	entries, _ := projreg.List(h)
	if len(entries) != 2 {
		t.Errorf("entries after prune = %+v, want live + minimal", entries)
	}
	// Prune never touches repository content (FR-028).
	if _, err := os.Stat(filepath.Join(live, "skills-lock.json")); err != nil {
		t.Error("prune touched a live project's files")
	}
}

func TestList_SkipsForeignFiles(t *testing.T) {
	t.Parallel()

	h := testHome(t)
	if err := os.WriteFile(filepath.Join(h.Root(), "projects", "junk.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.Root(), "projects", "README"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := projreg.List(h)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none (foreign files skipped)", entries)
	}
}
