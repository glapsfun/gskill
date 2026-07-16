package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/config"
)

// TestRegistry_RebuildsAfterDeletion (spec 015 US7 scenario 1, SC-009):
// deleting the whole registry breaks nothing — the next install succeeds and
// recreates the project's entry.
func TestRegistry_RebuildsAfterDeletion(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("install: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(h, "projects"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("registry entries after install = %v (%v), want 1", entries, err)
	}

	// Delete the registry entirely; the project must keep working.
	if err := os.RemoveAll(filepath.Join(h, "projects")); err != nil {
		t.Fatal(err)
	}
	if _, err := installLock(t, a, root, true); err != nil {
		t.Fatalf("install after registry deletion: %v", err)
	}
	entries, err = os.ReadDir(filepath.Join(h, "projects"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("registry not rebuilt: %v (%v)", entries, err)
	}

	// The entry lists the project and its references.
	infos, err := a.ProjectsList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Skills != 2 || infos[0].Root != root {
		t.Errorf("ProjectsList = %+v, want the project with 2 skills", infos)
	}
}

// TestRegistry_PruneRemovesStaleOnly (US7 scenario 2): prune drops entries
// for deleted projects and never touches live repositories.
func TestRegistry_PruneRemovesStaleOnly(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)

	live := t.TempDir()
	writeLockOnly(t, live, repo, ha, hb)
	if _, err := installLock(t, a, live, false); err != nil {
		t.Fatal(err)
	}

	doomed := t.TempDir()
	writeLockOnly(t, doomed, repo, ha, hb)
	if _, err := installLock(t, a, doomed, false); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(doomed); err != nil {
		t.Fatal(err)
	}

	removed, err := a.ProjectsPrune(context.Background())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 1 {
		t.Errorf("removed = %v, want the deleted project only", removed)
	}
	infos, _ := a.ProjectsList(context.Background())
	if len(infos) != 1 || infos[0].Root != live {
		t.Errorf("ProjectsList after prune = %+v, want only the live project", infos)
	}
	if _, err := os.Stat(filepath.Join(live, "skills-lock.json")); err != nil {
		t.Error("prune touched a live repository")
	}
}

// TestRegistry_MinimalPrivacyOmitsPaths (US7 scenario 3, FR-029).
func TestRegistry_MinimalPrivacyOmitsPaths(t *testing.T) {
	t.Parallel()

	h := filepath.Join(t.TempDir(), "gskill-home")
	cfg := config.Default()
	cfg.PrivacyProjectRegistry = config.PrivacyMinimal
	a := app.New(app.Options{
		Config:     cfg,
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: h,
	})

	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatal(err)
	}

	files, err := os.ReadDir(filepath.Join(h, "projects"))
	if err != nil || len(files) != 1 {
		t.Fatalf("registry entries = %v (%v)", files, err)
	}
	raw, err := os.ReadFile(filepath.Join(h, "projects", files[0].Name())) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), root) {
		t.Errorf("minimal entry leaks the project path:\n%s", raw)
	}
	var entry struct {
		References []struct {
			StoreHash string `json:"storeHash"`
		} `json:"references"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry.References) != 2 {
		t.Errorf("minimal entry lost references: %s", raw)
	}
}

// TestGC_EndToEnd_StaleRegistryLinksProtect (spec 015 US6 scenario 2 /
// integration scenario 12): even when the registry snapshot is stale (entry
// re-written with no references), the live active-link scan protects objects
// a project still uses; a truly unused object is collected.
func TestGC_EndToEnd_StaleRegistryLinksProtect(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatal(err)
	}
	used := listStoreObjects(t, h)
	if len(used) != 2 {
		t.Fatalf("store objects = %v", used)
	}

	staleRegistryEntry(t, h)

	// GC apply with a zero grace period: the project's live links must still
	// protect both objects.
	rep, err := a.StoreGC(context.Background(), true, 1)
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if len(rep.Deleted) != 0 {
		t.Errorf("GC deleted objects protected by live links: %v", rep.Deleted)
	}
	for _, obj := range used {
		if _, err := os.Stat(filepath.Join(h, "store", "sha256", obj)); err != nil {
			t.Errorf("object %s deleted despite live links", obj)
		}
	}

	// Remove the project (links gone, registry stale): now collection is
	// legitimate.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	rep, err = a.StoreGC(context.Background(), true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Deleted) != 2 {
		t.Errorf("Deleted = %v, want both unused objects", rep.Deleted)
	}
}

// staleRegistryEntry rewrites the single registry entry with an empty
// reference list, simulating a snapshot that predates the project's installs.
func staleRegistryEntry(t *testing.T, h string) {
	t.Helper()
	files, err := os.ReadDir(filepath.Join(h, "projects"))
	if err != nil || len(files) != 1 {
		t.Fatal(err)
	}
	entryPath := filepath.Join(h, "projects", files[0].Name())
	raw, err := os.ReadFile(entryPath) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(raw), `"references"`, `"referencesOld"`, 1)
	stale = strings.Replace(stale, "}\n", `,"references":[]}`+"\n", 1)
	var check map[string]any
	if err := json.Unmarshal([]byte(stale), &check); err != nil {
		t.Fatalf("stale entry construction failed: %v\n%s", err, stale)
	}
	if err := os.WriteFile(entryPath, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
}
