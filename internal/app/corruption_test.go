package app_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRepair_NoNetworkFromCloneCache: repair recovers a fully broken project
// (content, links, and state deleted; source gone) from the commit-keyed
// clone cache alone (spec 022 — the cache replaces the store as the offline
// recovery source).
func TestRepair_NoNetworkFromCloneCache(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	// Break everything project-side and delete the source: repair must
	// recover from the clone cache alone.
	for _, d := range []string{".agents", ".claude", filepath.Join(".gskill", "state.json")} {
		if err := os.RemoveAll(filepath.Join(root, d)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	res, err := a.Repair(t.Context(), root)
	if err != nil {
		t.Fatalf("repair with source deleted (store healthy): %v", err)
	}
	if len(res.Repaired) != 2 {
		t.Errorf("Repaired = %v, want both skills", res.Repaired)
	}
	assertAgentTargets(t, root, "alpha", "beta")
	for _, name := range []string{"alpha", "beta"} {
		if _, err := filepath.EvalSymlinks(filepath.Join(root, ".agents", "skills", name)); err != nil {
			t.Errorf("active link %s not recreated: %v", name, err)
		}
	}
}
