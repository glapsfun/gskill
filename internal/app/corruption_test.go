package app_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// tamperFirstObject rewrites SKILL.md inside every store object under home.
func tamperObjects(t *testing.T, h string) []string {
	t.Helper()
	objects := listStoreObjects(t, h)
	if len(objects) == 0 {
		t.Fatal("no store objects to tamper with")
	}
	for _, obj := range objects {
		victim := filepath.Join(h, "store", "sha256", obj, "content", "SKILL.md")
		if err := os.WriteFile(victim, []byte("# tampered\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return objects
}

// cloneProject copies seed's lockfile into a fresh project dir and deletes
// the source repo so any fetch attempt fails loudly.
func cloneProject(t *testing.T, seed, repo string) string {
	t.Helper()
	clone := t.TempDir()
	lockBytes, err := os.ReadFile(filepath.Join(seed, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "skills-lock.json"), lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if repo != "" {
		if err := os.RemoveAll(repo); err != nil {
			t.Fatal(err)
		}
	}
	return clone
}

// TestCorruption_TamperedObjectFailsClosedAndQuarantines is spec 015 US4
// scenario 1 and quickstart S4: a store object whose content no longer
// matches its identity is never activated — the install fails closed with
// the expected/actual hashes and a store-repair hint, and the object is
// quarantined.
func TestCorruption_TamperedObjectFailsClosedAndQuarantines(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	seed := t.TempDir()
	writeLockOnly(t, seed, repo, ha, hb)
	if _, err := installLock(t, a, seed, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}
	tamperObjects(t, h)

	// A fresh clone forced through the store: activation must refuse.
	clone := cloneProject(t, seed, "")

	res, err := a.InstallFromLock(t.Context(), installFromLockReq(clone, false, false))
	if err == nil {
		t.Fatal("install activated corrupted store content")
	}
	assertFailClosedIntegrity(t, res.Skills)
	assertQuarantinedNotActivated(t, h, clone)
}

// assertFailClosedIntegrity requires at least one skill failure that carries
// the hash pair and the store-repair hint.
func assertFailClosedIntegrity(t *testing.T, skills []app.LockSkillResult) {
	t.Helper()
	var sawIntegrity bool
	for _, s := range skills {
		if s.Err == nil || !errors.Is(s.Err, errs.ErrIntegrity) {
			continue
		}
		sawIntegrity = true
		msg := s.Err.Error() + errs.HintOf(s.Err)
		if !strings.Contains(msg, "sha256:") {
			t.Errorf("integrity error carries no hash: %q", msg)
		}
		if !strings.Contains(msg, "store repair") {
			t.Errorf("integrity error carries no repair hint: %q", msg)
		}
	}
	if !sawIntegrity {
		t.Fatalf("no fail-closed integrity error in %+v", skills)
	}
}

// assertQuarantinedNotActivated checks corruption ended in quarantine, never
// in an activated link.
func assertQuarantinedNotActivated(t *testing.T, h, clone string) {
	t.Helper()
	quarantined, err := os.ReadDir(filepath.Join(h, "quarantine"))
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) == 0 {
		t.Error("no quarantine entries after corruption")
	}
	if _, err := os.Lstat(filepath.Join(clone, ".agents", "skills", "alpha")); err == nil {
		resolved, rErr := filepath.EvalSymlinks(filepath.Join(clone, ".agents", "skills", "alpha"))
		if rErr == nil {
			data, _ := os.ReadFile(filepath.Join(resolved, "SKILL.md")) //nolint:gosec // test-controlled temp path
			if strings.Contains(string(data), "tampered") {
				t.Error("tampered content was activated")
			}
		}
	}
}

// TestRepair_NoNetworkFromGlobalStore is spec 015 T061 (contracts §1,
// FR-036): with a healthy global object and the source gone, `gskill repair`
// recreates the project-active link, agent links, and machine-local state —
// full recovery with zero network access.
func TestRepair_NoNetworkFromGlobalStore(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	// Break everything project-side and delete the source: repair must
	// recover from the store alone.
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

// TestVerify_UnsafeObjectPermissionsRefused (spec 015 FR-033, T028): an
// object writable by other users is refused at activation, fail closed.
func TestVerify_UnsafeObjectPermissionsRefused(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores permission checks differently")
	}
	t.Parallel()

	h, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	seed := t.TempDir()
	writeLockOnly(t, seed, repo, ha, hb)
	if _, err := installLock(t, a, seed, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}
	for _, obj := range listStoreObjects(t, h) {
		if err := os.Chmod(filepath.Join(h, "store", "sha256", obj), 0o777); err != nil { //nolint:gosec // intentional unsafe perms
			t.Fatal(err)
		}
	}

	clone := cloneProject(t, seed, repo)

	res, err := a.InstallFromLock(t.Context(), installFromLockReq(clone, true, true))
	if err == nil {
		t.Fatal("install activated an unsafely-permissioned store object")
	}
	for _, s := range res.Skills {
		if s.Err != nil && !strings.Contains(s.Err.Error(), "unsafe") {
			t.Errorf("error does not explain the unsafe permissions: %v", s.Err)
		}
	}
}
