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
	"github.com/glapsfun/gskill/internal/installer"
)

// scopeGlobal is the store-scope label asserted throughout these tests.
const scopeGlobal = "global"

// globalHome returns a private gskill home and an App bound to it.
func globalHome(t *testing.T) (string, *app.App) {
	t.Helper()
	h := filepath.Join(t.TempDir(), "gskill-home")
	return h, app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: h,
	})
}

// listStoreObjects returns the object hashes present in home's global store.
func listStoreObjects(t *testing.T, h string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(h, "store", "sha256"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// installLock runs InstallFromLock for root on a.
func installLock(t *testing.T, a *app.App, root string, offline bool) (app.InstallFromLockResult, error) {
	t.Helper()
	return a.InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Offline: offline,
	})
}

// TestGlobalReuse_TwoProjectsShareOneObject is spec 015 US1 scenarios 1–3 and
// quickstart S1: two projects locking identical content share exactly one
// global store object; the second project's install fetches nothing (the
// source repo is deleted first to prove it), both active links resolve into
// the store, state.json records the hash, and the committed lockfile carries
// no user-specific store path (FR-016, SC-010).
func TestGlobalReuse_TwoProjectsShareOneObject(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)

	repo1 := t.TempDir()
	writeLockOnly(t, repo1, repo, ha, hb)
	res1, err := installLock(t, a, repo1, false)
	if err != nil {
		t.Fatalf("repo1 install: %v", err)
	}
	assertStoreDecisions(t, "repo1", res1, installer.StoreDownloaded)
	objects := listStoreObjects(t, h)
	if len(objects) != 2 {
		t.Fatalf("store objects after repo1 = %v, want 2 (alpha, beta)", objects)
	}

	// repo2 is a fresh clone of the same project: same committed lockfile
	// (including the gskill block repo1's install enriched). The source repo
	// is deleted first — a successful install proves zero fetch (FR-006).
	repo2 := t.TempDir()
	lockBytes, err := os.ReadFile(filepath.Join(repo1, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo2, "skills-lock.json"), lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	res2, err := installLock(t, a, repo2, false)
	if err != nil {
		t.Fatalf("repo2 install (source deleted, store populated): %v", err)
	}
	assertStoreDecisions(t, "repo2", res2, installer.StoreReused)

	// Still exactly one physical object per skill (FR-005).
	objects = listStoreObjects(t, h)
	if len(objects) != 2 {
		t.Errorf("store objects after repo2 = %v, want still 2 (no duplicates)", objects)
	}

	for _, root := range []string{repo1, repo2} {
		assertActiveLinksIntoHome(t, root, h, "alpha", "beta")
		assertStateRecordsGlobal(t, root, "alpha", "beta")
		assertLockfileHasNoHomePath(t, root, h)
	}
}

// assertActiveLinksIntoHome checks every named skill's active link resolves
// under the global home.
func assertActiveLinksIntoHome(t *testing.T, root, h string, names ...string) {
	t.Helper()
	wantPrefix, _ := filepath.EvalSymlinks(h)
	for _, name := range names {
		link := filepath.Join(root, ".agents", "skills", name)
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatalf("%s active link: %v", root, err)
		}
		if !strings.HasPrefix(resolved, wantPrefix+string(filepath.Separator)) {
			t.Errorf("%s/%s resolves to %q, want under global home %q", root, name, resolved, wantPrefix)
		}
	}
}

// assertStateRecordsGlobal checks state.json records a project ID and, per
// skill, a store hash with global scope (FR-014).
func assertStateRecordsGlobal(t *testing.T, root string, names ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gskill", "state.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("%s state.json: %v", root, err)
	}
	var st struct {
		ProjectID string `json:"projectId"`
		Skills    map[string]struct {
			StoreHash  string `json:"storeHash"`
			StoreScope string `json:"storeScope"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("%s state.json: %v", root, err)
	}
	if st.ProjectID == "" {
		t.Errorf("%s state.json has no projectId", root)
	}
	for _, name := range names {
		sk, ok := st.Skills[name]
		if !ok || sk.StoreHash == "" {
			t.Errorf("%s state.json missing storeHash for %s", root, name)
		}
		if sk.StoreScope != scopeGlobal {
			t.Errorf("%s state.json %s storeScope = %q, want global store scope", root, name, sk.StoreScope)
		}
	}
}

// assertLockfileHasNoHomePath checks the committed lockfile carries no
// user-specific global path (FR-016, SC-010).
func assertLockfileHasNoHomePath(t *testing.T, root, h string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), h) {
		t.Errorf("%s lockfile contains the gskill home path:\n%s", root, raw)
	}
	if strings.Contains(string(raw), "/store/sha256/") {
		t.Errorf("%s lockfile contains an absolute store path:\n%s", root, raw)
	}
}

// TestGlobalReuse_CopyModeFromGlobalStore (spec 015 FR-012/FR-013, T060):
// forced copy materialization still sources verified content from the global
// store — agent targets are real directories matching the object, state.json
// records the copy mode and global scope, offline restore re-copies from the
// store without any fetch, and repairing a drifted copy never writes project
// changes back into the store.
func TestGlobalReuse_CopyModeFromGlobalStore(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	install := func(offline bool) (app.InstallFromLockResult, error) {
		return a.InstallFromLock(context.Background(), app.InstallFromLockRequest{
			Root: root, Agents: []string{testAgent}, InstallMode: "copy", Offline: offline,
		})
	}
	if _, err := install(false); err != nil {
		t.Fatalf("copy-mode install: %v", err)
	}

	target := filepath.Join(root, "."+testAgent, "skills", "alpha")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("agent target: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("agent target is a symlink, want a real copy")
	}

	assertCopyModeState(t, root)

	// Offline restore after deleting the copies: re-copied from the verified
	// global object, no fetch (the source repo is deleted to prove it).
	if err := os.RemoveAll(filepath.Join(root, "."+testAgent)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := install(true); err != nil {
		t.Fatalf("offline copy-mode restore from global store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatalf("restored copy missing SKILL.md: %v", err)
	}

	assertDriftRepairedFromStore(t, a, root, h, target)
	_ = ha
	_ = hb
}

// assertCopyModeState checks state.json records copy materialization with
// global scope for alpha.
func assertCopyModeState(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gskill", "state.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Skills map[string]struct {
			StoreScope string `json:"storeScope"`
			ActiveMode string `json:"activeMode"`
			Agents     map[string]struct {
				Mode string `json:"mode"`
			} `json:"agents"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	alpha := st.Skills["alpha"]
	if alpha.StoreScope != scopeGlobal {
		t.Errorf("storeScope = %q, want global store scope", alpha.StoreScope)
	}
	if alpha.Agents[testAgent].Mode != "copy" {
		t.Errorf("agent mode = %q, want copy", alpha.Agents[testAgent].Mode)
	}
}

// assertDriftRepairedFromStore drifts the project copy, syncs, and checks the
// copy was restored while the store object stayed byte-identical (FR-013).
// Drift repair is the reconcile paths' contract (sync/repair), not the
// install fast path's, which only relinks missing targets.
func assertDriftRepairedFromStore(t *testing.T, a *app.App, root, h, target string) {
	t.Helper()
	objects := listStoreObjects(t, h)
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("# drifted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync(context.Background(), app.SyncRequest{Root: root, Offline: true}); err != nil {
		t.Fatalf("sync over drifted copy: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(target, "SKILL.md")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(restored), "drifted") {
		t.Error("drifted copy was not repaired from the store")
	}
	for _, obj := range objects {
		content, err := os.ReadFile(filepath.Join(h, "store", "sha256", obj, "content", "SKILL.md")) //nolint:gosec // test-controlled temp path
		if err != nil {
			t.Fatalf("store object %s: %v", obj, err)
		}
		if strings.Contains(string(content), "drifted") {
			t.Error("project drift leaked back into the immutable store object")
		}
	}
}

// assertStoreDecisions checks every skill in res succeeded with the expected
// store decision and global scope (FR-007).
func assertStoreDecisions(t *testing.T, label string, res app.InstallFromLockResult, wantReuse string) {
	t.Helper()
	for _, s := range res.Skills {
		if s.Err != nil {
			t.Fatalf("%s %s: %v", label, s.Name, s.Err)
		}
		if s.StoreReuse != wantReuse {
			t.Errorf("%s %s StoreReuse = %q, want %q", label, s.Name, s.StoreReuse, wantReuse)
		}
		if s.StoreScope != scopeGlobal {
			t.Errorf("%s %s StoreScope = %q, want global store scope", label, s.Name, s.StoreScope)
		}
	}
}
