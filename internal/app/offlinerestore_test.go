package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// installFromLockReq builds the standard test request with frozen/offline
// toggles.
func installFromLockReq(root string, frozen, offline bool) app.InstallFromLockRequest {
	return app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Frozen: frozen, Offline: offline,
	}
}

// hintOf extracts the errs hint, or "".
func hintOf(err error) string { return errs.HintOf(err) }

// TestOfflineRestore_FrozenFromPopulatedStore is spec 015 US3 scenario 1 and
// quickstart S3: with every required object in the global store, a frozen +
// offline restore succeeds with zero network access (the source repo is
// deleted to prove it), repairs all links, and never mutates the lockfile.
func TestOfflineRestore_FrozenFromPopulatedStore(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	seed := t.TempDir()
	writeLockOnly(t, seed, repo, ha, hb)
	if _, err := installLock(t, a, seed, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	clone := t.TempDir()
	lockBytes, err := os.ReadFile(filepath.Join(seed, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "skills-lock.json"), lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err) // any fetch attempt now fails loudly
	}

	res, err := a.InstallFromLock(t.Context(), installFromLockReq(clone, true, true))
	if err != nil {
		t.Fatalf("frozen offline restore: %v", err)
	}
	for _, s := range res.Skills {
		if s.Err != nil {
			t.Fatalf("%s: %v", s.Name, s.Err)
		}
	}
	assertAgentTargets(t, clone, "alpha", "beta")

	after, err := os.ReadFile(filepath.Join(clone, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(lockBytes) {
		t.Error("frozen restore mutated the lockfile")
	}
}

// TestOfflineRestore_MissingObjectFailsWithRequiredHash is US3 scenario 2:
// offline with an object absent from a fresh machine's store fails closed,
// naming the skill, the required object identity, and the remediation.
func TestOfflineRestore_MissingObjectFailsWithRequiredHash(t *testing.T) {
	t.Parallel()

	_, seedApp := globalHome(t)
	repo, ha, hb := lockRepo(t)
	seed := t.TempDir()
	writeLockOnly(t, seed, repo, ha, hb)
	if _, err := installLock(t, seedApp, seed, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	// Fresh machine: same enriched lockfile, empty private home, no source.
	_, coldApp := globalHome(t)
	clone := t.TempDir()
	lockBytes, err := os.ReadFile(filepath.Join(seed, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "skills-lock.json"), lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	res, err := coldApp.InstallFromLock(t.Context(), installFromLockReq(clone, true, true))
	if err == nil {
		t.Fatal("offline restore with an empty store should fail")
	}
	var found bool
	for _, s := range res.Skills {
		if s.Err == nil {
			continue
		}
		found = true
		msg := s.Err.Error()
		if !strings.Contains(msg, s.Name) {
			t.Errorf("error does not name the skill: %q", msg)
		}
		if !strings.Contains(msg, "sha256:") {
			t.Errorf("error does not name the required object: %q", msg)
		}
	}
	if !found {
		t.Error("no per-skill failure recorded")
	}
	if !strings.Contains(err.Error()+hintOf(err), "--offline") {
		t.Errorf("no drop---offline remediation on %q", err)
	}
}

// TestFrozenRestore_MissingObjectFetchesExactCommit is US3 scenario 3
// (frozen without offline): a store miss fetches exactly the locked source.
func TestFrozenRestore_MissingObjectFetchesExactCommit(t *testing.T) {
	t.Parallel()

	_, seedApp := globalHome(t)
	repo, ha, hb := lockRepo(t)
	seed := t.TempDir()
	writeLockOnly(t, seed, repo, ha, hb)
	if _, err := installLock(t, seedApp, seed, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	// Fresh machine, source still available: frozen (not offline) fetches.
	freshHome, coldApp := globalHome(t)
	clone := t.TempDir()
	lockBytes, err := os.ReadFile(filepath.Join(seed, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "skills-lock.json"), lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := coldApp.InstallFromLock(t.Context(), installFromLockReq(clone, true, false)); err != nil {
		t.Fatalf("frozen restore with source available: %v", err)
	}
	if got := listStoreObjects(t, freshHome); len(got) != 2 {
		t.Errorf("fresh home store objects = %v, want the fetched pair", got)
	}
	assertAgentTargets(t, clone, "alpha", "beta")
}
