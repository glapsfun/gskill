package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/testutil"
)

// chmodTree sets mode on every regular file under dir, which is what a
// filesystem without POSIX permissions (WSL DrvFs) reports: every file reads
// back as executable. Symlinks and directories are left alone.
func chmodTree(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		return os.Chmod(p, mode)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestModeHash_PermissionlessCheckoutIsClean (spec 028 US1): a project whose
// installed skills read back with every file executable is still clean for
// verify, check, and a repeat install.
func TestModeHash_PermissionlessCheckoutIsClean(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	chmodTree(t, filepath.Join(root, ".agents", "skills"), 0o755)

	vr, err := a.Verify(t.Context(), root)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK {
		t.Errorf("verify not OK on a permission-less checkout: %+v", vr.Skills)
	}
	for _, s := range vr.Skills {
		if s.Issue != "ok" {
			t.Errorf("verify %s = %q, want ok", s.Name, s.Issue)
		}
	}

	cr, err := a.Check(t.Context(), root, true)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if cr.HasDrift {
		t.Errorf("check reports drift on a permission-less checkout: %+v", cr.Skills)
	}

	if _, err := installLock(t, a, root, false); err != nil {
		t.Errorf("repeat install on a permission-less checkout: %v", err)
	}
}

// lockBytes returns the raw skills-lock.json of a project.
func lockBytes(t *testing.T, root string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// contentHashes maps each skill to the gskill.contentHash its lock records.
func contentHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	var doc struct {
		Skills map[string]struct {
			Gskill struct {
				ContentHash string `json:"contentHash"`
			} `json:"gskill"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(lockBytes(t, root), &doc); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(doc.Skills))
	for name, e := range doc.Skills {
		out[name] = e.Gskill.ContentHash
	}
	return out
}

// shareDeclarations copies what a teammate clones: the lock, and the
// manifest when one exists.
func shareDeclarations(t *testing.T, from, to string) {
	t.Helper()
	for _, name := range []string{skillslock.FileName, "skills.toml"} {
		b, err := os.ReadFile(filepath.Join(from, name)) //nolint:gosec // test-controlled temp path
		if os.IsNotExist(err) && name != skillslock.FileName {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// assertClean fails unless verify and a drift-failing check both pass.
func assertClean(t *testing.T, a *app.App, root string) {
	t.Helper()
	vr, err := a.Verify(t.Context(), root)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK {
		t.Errorf("verify not OK: %+v", vr.Skills)
	}
	cr, err := a.Check(t.Context(), root, true)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if cr.HasDrift {
		t.Errorf("check reports drift: %+v", cr.Skills)
	}
}

// TestModeHash_LockFromNormalFSRestoresOnPermissionless (spec 028 US2): a lock
// written on a normal filesystem restores, verifies, and re-restores cleanly
// in a checkout that reads every file back as executable.
func TestModeHash_LockFromNormalFSRestoresOnPermissionless(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	rootA := t.TempDir()
	writeLockOnly(t, rootA, repo, ha, hb)
	if _, err := installLock(t, a, rootA, false); err != nil {
		t.Fatalf("project A install: %v", err)
	}

	rootB := t.TempDir()
	shareDeclarations(t, rootA, rootB)
	if _, err := a.InstallFromLock(t.Context(), installFromLockReq(rootB, true, false)); err != nil {
		t.Fatalf("project B frozen restore: %v", err)
	}
	chmodTree(t, filepath.Join(rootB, ".agents", "skills"), 0o755)
	assertClean(t, a, rootB)

	before := lockBytes(t, rootB)
	if _, err := a.InstallFromLock(t.Context(), installFromLockReq(rootB, true, false)); err != nil {
		t.Fatalf("project B second frozen restore: %v", err)
	}
	if string(lockBytes(t, rootB)) != string(before) {
		t.Error("a frozen restore modified the lock")
	}
}

// TestModeHash_LockFromPermissionlessRestoresOnNormalFS (spec 028 US2): the
// reverse direction. A lock rewritten from a checkout that reads every file
// back as executable records the same hashes and restores on a normal one.
func TestModeHash_LockFromPermissionlessRestoresOnNormalFS(t *testing.T) {
	t.Parallel()

	_, a := globalHome(t)
	repo, ha, hb := lockRepo(t)
	rootA := t.TempDir()
	writeLockOnly(t, rootA, repo, ha, hb)
	if _, err := installLock(t, a, rootA, false); err != nil {
		t.Fatalf("project A install: %v", err)
	}
	chmodTree(t, filepath.Join(rootA, ".agents", "skills"), 0o755)
	if _, err := installLock(t, a, rootA, false); err != nil {
		t.Fatalf("project A install on a permission-less checkout: %v", err)
	}

	rootB := t.TempDir()
	shareDeclarations(t, rootA, rootB)
	if _, err := a.InstallFromLock(t.Context(), installFromLockReq(rootB, true, false)); err != nil {
		t.Fatalf("project B frozen restore: %v", err)
	}
	assertClean(t, a, rootB)

	ga, gb := contentHashes(t, rootA), contentHashes(t, rootB)
	for name, h := range ga {
		if h == "" || gb[name] != h {
			t.Errorf("%s contentHash: A=%q B=%q, want identical and non-empty", name, h, gb[name])
		}
	}
}

// execLockRepo is lockRepo with an executable helper script in alpha, the
// case whose pre-028 content hash depended on the owner execute bit. Beta has
// no executable file, so its hash is the same under both rules.
func execLockRepo(t *testing.T) (repo, hashAlpha, hashBeta string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	files := map[string]string{
		"skills/alpha/SKILL.md":       "---\nname: alpha\ndescription: The alpha skill\n---\n# alpha\n",
		"skills/alpha/scripts/run.sh": "#!/bin/sh\necho ok\n",
		"skills/beta/SKILL.md":        "---\nname: beta\ndescription: The beta skill\n---\n# beta\n",
	}
	for rel, body := range files {
		full := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(repo, "skills", skillAlpha, "scripts", "run.sh"), 0o755); err != nil { //nolint:gosec // the fixture needs an executable script
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--quiet", "-b", "main"}, {"add", "."}, {"commit", "--quiet", "-m", "skills"}} {
		cmd := exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // test-controlled fixture args
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	var err error
	if hashAlpha, err = integrity.CompatHash(filepath.Join(repo, "skills", skillAlpha)); err != nil {
		t.Fatal(err)
	}
	if hashBeta, err = integrity.CompatHash(filepath.Join(repo, "skills", "beta")); err != nil {
		t.Fatal(err)
	}
	return repo, hashAlpha, hashBeta
}

// legacyizeLock rewrites name's gskill.contentHash to the value a pre-028
// release would have recorded for the installed copy as it reads back now.
// It edits the hash in place so the rest of the file keeps gskill's own
// formatting, and refuses to produce a vacuous fixture.
func legacyizeLock(t *testing.T, root, name string) {
	t.Helper()
	h, err := integrity.HashDir(filepath.Join(root, ".agents", "skills", name))
	if err != nil {
		t.Fatal(err)
	}
	if h.LegacyContentHash == h.ContentHash {
		t.Fatalf("%s: legacy and current hashes are equal; the fixture has no executable file to exercise", name)
	}
	current := contentHashes(t, root)[name]
	if current == "" {
		t.Fatalf("%s has no gskill.contentHash to legacyize", name)
	}
	lock := lockBytes(t, root)
	if n := bytes.Count(lock, []byte(current)); n != 1 {
		t.Fatalf("%s contentHash %s appears %d times in the lock, want exactly 1", name, current, n)
	}
	lock = bytes.Replace(lock, []byte(current), []byte(h.LegacyContentHash), 1)
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), lock, 0o600); err != nil {
		t.Fatal(err)
	}
}

// entryRaw returns one lock entry exactly as written.
func entryRaw(t *testing.T, root, name string) string {
	t.Helper()
	var doc struct {
		Skills map[string]json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(lockBytes(t, root), &doc); err != nil {
		t.Fatal(err)
	}
	return string(doc.Skills[name])
}

// currentHash is the spec 028 content hash of an installed skill.
func currentHash(t *testing.T, root, name string) string {
	t.Helper()
	h, err := integrity.HashDir(filepath.Join(root, ".agents", "skills", name))
	if err != nil {
		t.Fatal(err)
	}
	return h.ContentHash
}

// execProject installs execLockRepo into a fresh project.
func execProject(t *testing.T) (*app.App, string) {
	t.Helper()
	_, a := globalHome(t)
	repo, ha, hb := execLockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("seed install: %v", err)
	}
	return a, root
}

// assertLockUnchanged fails if the lock differs from snapshot after step.
func assertLockUnchanged(t *testing.T, root string, snapshot []byte, step string) {
	t.Helper()
	if !bytes.Equal(lockBytes(t, root), snapshot) {
		t.Errorf("%s modified the lock; it must accept a pre-028 entry without rewriting it", step)
	}
}

// TestModeHash_LegacyLockReadOnlyIsClean (spec 028 US3): a pre-028 entry is
// accepted by verify, check, and a frozen install, none of which rewrite it.
func TestModeHash_LegacyLockReadOnlyIsClean(t *testing.T) {
	t.Parallel()

	a, root := execProject(t)
	legacyizeLock(t, root, skillAlpha)
	snapshot := lockBytes(t, root)

	assertClean(t, a, root)
	assertLockUnchanged(t, root, snapshot, "verify/check")
	if _, err := a.InstallFromLock(t.Context(), installFromLockReq(root, true, false)); err != nil {
		t.Fatalf("frozen install with a pre-028 entry: %v", err)
	}
	assertLockUnchanged(t, root, snapshot, "a frozen install")
}

// TestModeHash_LegacyEntryUpgradedOnce (spec 028 FR-007, SC-004): a writing
// install moves a pre-028 entry to the current hash once, leaves entries
// without executable files byte-identical, and is stable afterwards.
func TestModeHash_LegacyEntryUpgradedOnce(t *testing.T) {
	t.Parallel()

	a, root := execProject(t)
	legacyizeLock(t, root, skillAlpha)
	betaBefore := entryRaw(t, root, "beta")
	snapshot := lockBytes(t, root)

	plan, err := a.InstallFromLock(t.Context(), app.InstallFromLockRequest{Root: root, Agents: []string{testAgent}, DryRun: true})
	if err != nil {
		t.Fatalf("dry run with a pre-028 entry: %v", err)
	}
	assertLockUnchanged(t, root, snapshot, "a dry run")
	for _, s := range plan.Skills {
		if s.Name == skillAlpha && (s.Status != app.LockSkillPlanned || s.PlannedAction != app.PlannedWouldUpdateLock) {
			t.Errorf("dry run alpha = %s/%s, want planned %s", s.Status, s.PlannedAction, app.PlannedWouldUpdateLock)
		}
	}

	res, err := installLock(t, a, root, false)
	if err != nil {
		t.Fatalf("install with a pre-028 entry: %v", err)
	}
	if !res.Changed {
		t.Error("install rewrote the lock but reported Changed=false")
	}
	if got, want := contentHashes(t, root)[skillAlpha], currentHash(t, root, skillAlpha); got != want {
		t.Errorf("alpha contentHash = %s, want it upgraded to %s", got, want)
	}
	if got := entryRaw(t, root, "beta"); got != betaBefore {
		t.Errorf("beta entry changed:\nbefore %s\nafter  %s", betaBefore, got)
	}

	upgraded := lockBytes(t, root)
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("second install: %v", err)
	}
	assertLockUnchanged(t, root, upgraded, "a second install")
}

// TestModeHash_LegacyEditStillDetected (spec 028 FR-008, SC-005): accepting
// pre-028 hashes never hides a real edit, for pre-028 and current entries.
func TestModeHash_LegacyEditStillDetected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		legacy bool
	}{{"pre-028 entry", true}, {"current entry", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, root := execProject(t)
			if tc.legacy {
				legacyizeLock(t, root, skillAlpha)
			}
			appendToFile(t, filepath.Join(root, ".agents", "skills", skillAlpha, "SKILL.md"), "x")
			assertVerifyMismatch(t, a, root, skillAlpha)
			assertInstallDrift(t, a, root, skillAlpha)
		})
	}
}

func appendToFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// assertVerifyMismatch expects verify to fail closed on name and report the
// edited tree's current hash as the observed value.
func assertVerifyMismatch(t *testing.T, a *app.App, root, name string) {
	t.Helper()
	vr, err := a.Verify(t.Context(), root)
	if !errors.Is(err, errs.ErrIntegrity) {
		t.Errorf("verify error = %v, want ErrIntegrity (fail closed)", err)
	}
	for _, s := range vr.Skills {
		if s.Name == name && (s.Issue != "mismatch" || s.Actual != currentHash(t, root, name)) {
			t.Errorf("verify %s = %q actual %s, want mismatch with the edited tree's hash", name, s.Issue, s.Actual)
		}
	}
}

// assertInstallDrift expects install to refuse name with the drift error.
func assertInstallDrift(t *testing.T, a *app.App, root, name string) {
	t.Helper()
	res, err := installLock(t, a, root, false)
	if err == nil {
		t.Error("install over an edited copy succeeded, want it to fail")
	}
	var skillErr error
	for _, s := range res.Skills {
		if s.Name == name {
			skillErr = s.Err
		}
	}
	if !errors.Is(skillErr, errs.ErrInvalidLock) || !strings.Contains(skillErr.Error(), "no longer matches skills-lock.json") {
		t.Errorf("%s install error = %v, want the drift error", name, skillErr)
	}
}

// TestModeHash_LegacyRestoreFromCache (spec 028 FR-006, FR-013): a pre-028
// entry restores from the cache; a frozen restore keeps the lock, a writing
// install upgrades it, and sync never rewrites a fingerprint.
func TestModeHash_LegacyRestoreFromCache(t *testing.T) {
	t.Parallel()

	a, root := execProject(t)
	alpha := filepath.Join(root, ".agents", "skills", skillAlpha)
	legacyizeLock(t, root, skillAlpha)
	snapshot := lockBytes(t, root)

	if err := os.RemoveAll(alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := a.InstallFromLock(t.Context(), installFromLockReq(root, true, false)); err != nil {
		t.Fatalf("frozen restore of a pre-028 entry: %v", err)
	}
	assertLockUnchanged(t, root, snapshot, "a frozen restore")

	if err := os.RemoveAll(alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("restore of a pre-028 entry: %v", err)
	}
	if got, want := contentHashes(t, root)[skillAlpha], currentHash(t, root, skillAlpha); got != want {
		t.Errorf("alpha contentHash after restore = %s, want %s", got, want)
	}

	legacyizeLock(t, root, skillAlpha)
	legacyValue := contentHashes(t, root)[skillAlpha]
	if _, err := a.Sync(t.Context(), app.SyncRequest{Root: root}); err != nil {
		t.Fatalf("sync with a pre-028 entry: %v", err)
	}
	if got := contentHashes(t, root)[skillAlpha]; got != legacyValue {
		t.Errorf("sync rewrote alpha's contentHash to %s; sync must never rewrite fingerprints", got)
	}
}

// TestModeHash_LegacyLockFromPermissionlessCheckout (spec 028 US3 scenario 6):
// a lock an old release wrote on a WSL drive recorded every file as
// executable. It stays clean there, and a writing install upgrades it.
func TestModeHash_LegacyLockFromPermissionlessCheckout(t *testing.T) {
	t.Parallel()

	a, root := execProject(t)
	chmodTree(t, filepath.Join(root, ".agents", "skills"), 0o755)
	legacyizeLock(t, root, skillAlpha)
	legacyizeLock(t, root, "beta")
	snapshot := lockBytes(t, root)

	assertClean(t, a, root)
	if _, err := a.InstallFromLock(t.Context(), installFromLockReq(root, true, false)); err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	assertLockUnchanged(t, root, snapshot, "verify/check/frozen install")

	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	got := contentHashes(t, root)
	for _, name := range []string{skillAlpha, "beta"} {
		if want := currentHash(t, root, name); got[name] != want {
			t.Errorf("%s contentHash = %s, want it upgraded to %s", name, got[name], want)
		}
	}
}

// TestModeHash_LegacyEntryOnPermissionlessCheckoutNeedsOneInstall pins the
// documented limitation (research R7) and its remedy: a pre-028 entry written
// on a normal filesystem for a skill that ships an executable file cannot
// match in place on a checkout that reads every file back as executable, and
// one plain install rewrites it so everything is clean from then on.
func TestModeHash_LegacyEntryOnPermissionlessCheckoutNeedsOneInstall(t *testing.T) {
	t.Parallel()

	a, root := execProject(t)
	legacyizeLock(t, root, skillAlpha)
	chmodTree(t, filepath.Join(root, ".agents", "skills"), 0o755)

	if cr, err := a.Check(t.Context(), root, false); err != nil || !cr.HasDrift {
		t.Fatalf("check before the rewrite = drift:%v err:%v; the limitation no longer reproduces, update the docs", cr.HasDrift, err)
	}
	if _, err := installLock(t, a, root, false); err != nil {
		t.Fatalf("install on the permission-less checkout: %v", err)
	}
	if got, want := contentHashes(t, root)[skillAlpha], currentHash(t, root, skillAlpha); got != want {
		t.Errorf("alpha contentHash = %s, want it rewritten to %s", got, want)
	}
	assertClean(t, a, root)
}
