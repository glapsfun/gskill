package migrate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/home"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/migrate"
)

// newGlobal returns an ensured global store over a private home.
func newGlobal(t *testing.T) *globalstore.Store {
	t.Helper()
	h := home.New(filepath.Join(t.TempDir(), "gskill-home"))
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	return globalstore.New(h)
}

// seedLocalObject writes a skill into a project's legacy store, returning
// its content key.
func seedLocalObject(t *testing.T, root, body string) string {
	t.Helper()
	tmp := t.TempDir()
	md := "---\nname: skill\ndescription: legacy object\n---\n" + body
	if err := os.WriteFile(filepath.Join(tmp, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
	hashes, err := integrity.HashDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	key := hashes.ContentHash
	dest := filepath.Join(root, ".gskill", "store", "sha256", key[len("sha256:"):])
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dest, os.DirFS(tmp)); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestBuildPlan_CountsAndSavings(t *testing.T) {
	t.Parallel()

	gs := newGlobal(t)
	root := t.TempDir()
	keyShared := seedLocalObject(t, root, "# shared "+t.Name()+"\n")
	keyNew := seedLocalObject(t, root, "# only local "+t.Name()+"\n")

	// keyShared already exists globally.
	if _, err := gs.Admit(t.Context(), keyShared,
		filepath.Join(root, ".gskill", "store", "sha256", keyShared[len("sha256:"):]),
		globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}

	plan, err := migrate.BuildPlan(root, gs)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.LocalObjects != 2 || plan.AlreadyGlobal != 1 || plan.ToCopy != 1 {
		t.Errorf("plan = %+v, want 2 local / 1 global / 1 to copy", plan)
	}
	if plan.SavingsBytes <= 0 {
		t.Errorf("SavingsBytes = %d, want positive (dedup frees space)", plan.SavingsBytes)
	}
	// Dry-run: nothing changed.
	if gs.Has(keyNew) {
		t.Error("BuildPlan admitted an object")
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "store")); err != nil {
		t.Error("BuildPlan touched the local store")
	}
}

func TestBuildPlan_ReportsCorruptLocalObjects(t *testing.T) {
	t.Parallel()

	gs := newGlobal(t)
	root := t.TempDir()
	key := seedLocalObject(t, root, "# will corrupt "+t.Name()+"\n")
	victim := filepath.Join(root, ".gskill", "store", "sha256", key[len("sha256:"):], "SKILL.md")
	if err := os.WriteFile(victim, []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := migrate.BuildPlan(root, gs)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Corrupt) != 1 || plan.Corrupt[0] != key {
		t.Errorf("Corrupt = %v, want [%s]", plan.Corrupt, key)
	}
}

func TestRun_MigratesRelinksAndRemovesLocalStore(t *testing.T) {
	t.Parallel()

	gs := newGlobal(t)
	root := t.TempDir()
	key := seedLocalObject(t, root, "# migrating "+t.Name()+"\n")

	// A pre-existing active link into the legacy store (the state migration
	// must re-point).
	legacyContent := filepath.Join(root, ".gskill", "store", "sha256", key[len("sha256:"):])
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(legacyContent, filepath.Join(root, ".agents", "skills", "skill")); err != nil {
		t.Fatal(err)
	}

	res, err := migrate.Run(t.Context(), root, gs, []migrate.LockedSkill{
		{Name: "skill", ContentHash: key, Origin: globalstore.Origin{Commit: "abc"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !gs.Has(key) {
		t.Error("object not admitted globally")
	}
	if res.AdmittedObjects != 1 || len(res.Relinked) != 1 {
		t.Errorf("res = %+v, want 1 admitted / 1 relinked", res)
	}
	if !res.LocalStoreRemoved {
		t.Error("local store not removed after complete success")
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "store")); !os.IsNotExist(err) {
		t.Error("legacy store still on disk")
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, ".agents", "skills", "skill"))
	if err != nil {
		t.Fatal(err)
	}
	wantContent, _ := filepath.EvalSymlinks(gs.ContentPath(key))
	if target != wantContent {
		t.Errorf("active link -> %q, want global %q", target, wantContent)
	}
}

func TestRun_CorruptObjectSkippedLocalStorePreserved(t *testing.T) {
	t.Parallel()

	gs := newGlobal(t)
	root := t.TempDir()
	good := seedLocalObject(t, root, "# good "+t.Name()+"\n")
	bad := seedLocalObject(t, root, "# bad "+t.Name()+"\n")
	victim := filepath.Join(root, ".gskill", "store", "sha256", bad[len("sha256:"):], "SKILL.md")
	if err := os.WriteFile(victim, []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := migrate.Run(t.Context(), root, gs, []migrate.LockedSkill{
		{Name: "good", ContentHash: good},
		{Name: "bad", ContentHash: bad},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !gs.Has(good) {
		t.Error("healthy object not migrated")
	}
	if gs.Has(bad) {
		t.Error("corrupt object was admitted")
	}
	if res.LocalStoreRemoved {
		t.Error("local store removed despite a skipped object (FR-038)")
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "store")); err != nil {
		t.Error("legacy store missing after partial migration")
	}
}

func TestRun_FailureLeavesLocalStoreUsable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses the injected permission failure")
	}
	t.Parallel()

	gs := newGlobal(t)
	root := t.TempDir()
	key := seedLocalObject(t, root, "# blocked "+t.Name()+"\n")

	// Injected failure: the global store root is read-only, so admission
	// cannot create the object directory.
	if err := os.Chmod(gs.Home().StoreDir(), 0o500); err != nil { //nolint:gosec // intentional for failure injection
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gs.Home().StoreDir(), 0o700) }) //nolint:gosec // restore perms after failure injection

	_, err := migrate.Run(t.Context(), root, gs, []migrate.LockedSkill{
		{Name: "skill", ContentHash: key},
	})
	if err == nil {
		t.Fatal("Run succeeded against a read-only global store")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".gskill", "store", "sha256", key[len("sha256:"):], "SKILL.md")); statErr != nil {
		t.Errorf("local store damaged by failed migration: %v", statErr)
	}
}
