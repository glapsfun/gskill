package app_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/config"
)

// legacyProject installs repo's skills with the project-local store forced,
// returning the project root. The resulting layout is a pre-spec-015 project.
func legacyProject(t *testing.T, gskillHome string) (root string) {
	t.Helper()
	repo, ha, hb := lockRepo(t)
	root = t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	cfg := config.Default()
	cfg.StoreScope = config.StoreScopeProject
	legacy := app.New(app.Options{
		Config:     cfg,
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: gskillHome,
	})
	if _, err := legacy.InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent},
	}); err != nil {
		t.Fatalf("legacy install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "store", "sha256")); err != nil {
		t.Fatalf("legacy project has no local store: %v", err)
	}
	return root
}

// TestMigrate_DryRunChangesNothing (spec 015 US5 scenario 1): the plan is
// reported and the tree stays byte-identical.
func TestMigrate_DryRunChangesNothing(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	root := legacyProject(t, h)
	before := digestTree(t, root)
	beforeStore := digestTree(t, filepath.Join(root, ".gskill", "store"))

	rep, err := a.MigrateGlobalStore(context.Background(), root, true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if rep.NothingToDo {
		t.Fatal("dry-run reported nothing to do for a legacy project")
	}
	if rep.Plan.LocalObjects != 2 || rep.Plan.ToCopy != 2 {
		t.Errorf("plan = %+v, want 2 local / 2 to copy", rep.Plan)
	}
	if got := digestTree(t, root); got != before {
		t.Error("dry-run modified the project tree")
	}
	if got := digestTree(t, filepath.Join(root, ".gskill", "store")); got != beforeStore {
		t.Error("dry-run modified the local store")
	}
	if got := listStoreObjects(t, h); len(got) != 0 {
		t.Errorf("dry-run admitted objects globally: %v", got)
	}
}

// TestMigrate_RelinksAndRemovesLocalStore (US5 scenario 2): content is
// preserved globally, links re-pointed, state recorded, legacy store removed,
// and the project verifies offline afterwards.
func TestMigrate_RelinksAndRemovesLocalStore(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	root := legacyProject(t, h)

	rep, err := a.MigrateGlobalStore(context.Background(), root, false)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !rep.Result.LocalStoreRemoved {
		t.Fatal("legacy store not removed after complete success")
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "store")); !os.IsNotExist(err) {
		t.Error("legacy store still present")
	}
	if got := listStoreObjects(t, h); len(got) != 2 {
		t.Errorf("global objects = %v, want 2 migrated", got)
	}
	assertActiveLinksIntoHome(t, root, h, "alpha", "beta")
	assertStateRecordsGlobal(t, root, "alpha", "beta")
	assertAgentTargets(t, root, "alpha", "beta")

	// The migrated project restores offline from the global store.
	if _, err := installLock(t, a, root, true); err != nil {
		t.Errorf("offline install after migration: %v", err)
	}

	// Re-running is a no-op success (already migrated).
	again, err := a.MigrateGlobalStore(context.Background(), root, false)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if !again.NothingToDo {
		t.Error("second migration did not report nothing-to-do")
	}
}

// TestMigrate_FailureLeavesProjectUsable (US5 scenario 3, FR-038): an
// injected failure (read-only global store) aborts with the local store and
// links intact — the project keeps working from its legacy layout.
func TestMigrate_FailureLeavesProjectUsable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses the injected permission failure")
	}
	t.Parallel()

	h, a := globalHome(t)
	root := legacyProject(t, h)

	// The legacy install never touched the home; create it so the failure
	// can be injected on its store directory.
	storeDir := filepath.Join(h, "store")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(storeDir, 0o500); err != nil { //nolint:gosec // failure injection
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(storeDir, 0o700) }) //nolint:gosec // restore perms after failure injection

	if _, err := a.MigrateGlobalStore(context.Background(), root, false); err == nil {
		t.Fatal("migration succeeded against a read-only global store")
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "store", "sha256")); err != nil {
		t.Errorf("legacy store damaged by failed migration: %v", err)
	}
	// The project still restores offline from its legacy layout: scope
	// auto-detection sees the populated local store and stays project-scoped.
	if _, err := installLock(t, a, root, true); err != nil {
		t.Errorf("project unusable after failed migration: %v", err)
	}
}
