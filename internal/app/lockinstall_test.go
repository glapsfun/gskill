package app_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// testAgent is the agent every lock-install test targets.
const testAgent = "claude"

// lockApp is the lock-install tests' name for the shared test App constructor.
func lockApp() *app.App { return onboardApp() }

func runLockInstall(t *testing.T, root string) (app.InstallFromLockResult, error) {
	t.Helper()
	return lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root:   root,
		Agents: []string{testAgent},
	})
}

// lockRepo builds a git repo with two skills under skills/alpha and
// skills/beta, returning the repo path and each skill's compat hash.
func lockRepo(t *testing.T) (repo, hashAlpha, hashBeta string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(repo, "skills", name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: The " + name + " skill\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "-b", "main")
	run("add", ".")
	run("commit", "--quiet", "-m", "skills")

	var err error
	if hashAlpha, err = integrity.CompatHash(filepath.Join(repo, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	if hashBeta, err = integrity.CompatHash(filepath.Join(repo, "skills", "beta")); err != nil {
		t.Fatal(err)
	}
	return repo, hashAlpha, hashBeta
}

// writeLockOnly writes a lock-only project dir: skills-lock.json (with foreign
// data) and nothing else.
func writeLockOnly(t *testing.T, root, repo, hashAlpha, hashBeta string) {
	t.Helper()
	lock := `{
  "version": 1,
  "customTopLevel": "keep-me",
  "skills": {
    "alpha": {
      "source": ` + jsonStr(repo) + `,
      "sourceType": "local",
      "skillPath": "skills/alpha/SKILL.md",
      "computedHash": "` + hashAlpha + `",
      "otherTool": {"pin": "v1"}
    },
    "beta": {
      "source": ` + jsonStr(repo) + `,
      "sourceType": "local",
      "skillPath": "skills/beta/SKILL.md",
      "computedHash": "` + hashBeta + `"
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
}

func jsonStr(s string) string {
	return strconv.Quote(s) // JSON-compatible escaping for paths
}

// assertProjectScaffold checks auto-init results (FR-019).
func assertProjectScaffold(t *testing.T, root string) {
	t.Helper()
	for _, f := range []string{".gskill", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("%s missing after auto-init: %v", f, err)
		}
	}
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test-controlled temp path
	if !strings.Contains(string(gi), ".gskill/") {
		t.Errorf(".gitignore lacks .gskill/ entry:\n%s", gi)
	}
}

// assertAgentTargets checks agent placements exist for the test agent.
func assertAgentTargets(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(root, "."+testAgent, "skills", name)); err != nil {
			t.Errorf("agent target for %s missing: %v", name, err)
		}
	}
}

// assertLockEnriched checks gskill blocks and preserved data after install.
func assertLockEnriched(t *testing.T, root string, wantHashes map[string]string) {
	t.Helper()
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatalf("reload lock: %v", err)
	}
	for name, wantHash := range wantHashes {
		e, ok := l.Entry(name)
		if !ok {
			t.Fatalf("entry %s missing after install", name)
		}
		if e.ComputedHash != wantHash {
			t.Errorf("%s computedHash = %q, want unchanged %q", name, e.ComputedHash, wantHash)
		}
		if e.Ext == nil {
			t.Fatalf("%s gskill block missing", name)
		}
		if len(e.Ext.Agents) != 1 || e.Ext.Agents[0] != testAgent {
			t.Errorf("%s Ext.Agents = %v", name, e.Ext.Agents)
		}
		if e.Ext.StoreHash == "" {
			t.Errorf("%s Ext.StoreHash empty", name)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	for _, want := range []string{`"customTopLevel": "keep-me"`, `"otherTool": {`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("foreign data %q lost:\n%s", want, raw)
		}
	}
}

// TestInstallFromLock_LockOnlyDirectory is US1's core journey: a directory
// containing only skills-lock.json becomes a fully installed project.
func TestInstallFromLock_LockOnlyDirectory(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	res, err := runLockInstall(t, root)
	if err != nil {
		t.Fatalf("InstallFromLock: %v", err)
	}
	if !res.Initialized {
		t.Error("Initialized = false, want auto-init")
	}
	if !res.Changed {
		t.Error("Changed = false on first install")
	}
	if len(res.Skills) != 2 {
		t.Fatalf("Skills = %+v, want 2", res.Skills)
	}
	for _, s := range res.Skills {
		if s.Status != app.LockSkillInstalled {
			t.Errorf("skill %s status = %q, want installed (%v)", s.Name, s.Status, s.Err)
		}
	}
	assertProjectScaffold(t, root)
	assertAgentTargets(t, root, "alpha", "beta")
	assertLockEnriched(t, root, map[string]string{"alpha": ha, "beta": hb})
}

func assertPartialOutcome(t *testing.T, root string, res app.InstallFromLockResult) {
	t.Helper()
	byName := map[string]app.LockSkillResult{}
	for _, s := range res.Skills {
		byName[s.Name] = s
	}
	if byName[skillAlpha].Status != app.LockSkillInstalled {
		t.Errorf("alpha status = %q, want installed (%v)", byName[skillAlpha].Status, byName[skillAlpha].Err)
	}
	if byName["beta"].Status != app.LockSkillFailed {
		t.Errorf("beta status = %q, want failed", byName["beta"].Status)
	}
	if byName["beta"].Err == nil || !errors.Is(byName["beta"].Err, errs.ErrIntegrity) {
		t.Errorf("beta error = %v, want integrity failure", byName["beta"].Err)
	}
	if _, err := os.Stat(filepath.Join(root, "."+testAgent, "skills", "alpha")); err != nil {
		t.Errorf("alpha target missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "."+testAgent, "skills", "beta")); err == nil {
		t.Error("beta was activated despite hash mismatch")
	}
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatalf("reload lock: %v", err)
	}
	if e, _ := l.Entry(skillAlpha); e.Ext == nil {
		t.Error("alpha gskill block missing after partial failure")
	}
	if e, _ := l.Entry("beta"); e.Ext != nil {
		t.Error("beta gained a gskill block despite failing")
	}
}

// TestInstallFromLock_PartialFailure (T019/FR-016a, clarification Q2): verified
// successes stay recorded, failures are reported per skill, and a re-run
// retries only the failed skill.
func TestInstallFromLock_PartialFailure(t *testing.T) {
	t.Parallel()
	repo, ha, _ := lockRepo(t)
	root := t.TempDir()
	bogus := strings.Repeat("0", 64)
	writeLockOnly(t, root, repo, ha, bogus) // beta's hash corrupted

	res, err := runLockInstall(t, root)
	if !errors.Is(err, errs.ErrPartialInstall) {
		t.Fatalf("err = %v, want ErrPartialInstall", err)
	}
	assertPartialOutcome(t, root, res)

	// Re-run: alpha succeeds again (incremental), beta still fails.
	res2, err2 := runLockInstall(t, root)
	if !errors.Is(err2, errs.ErrPartialInstall) {
		t.Fatalf("re-run err = %v, want ErrPartialInstall", err2)
	}
	for _, s := range res2.Skills {
		if s.Name == "beta" && s.Status != app.LockSkillFailed {
			t.Errorf("re-run beta status = %q, want failed", s.Status)
		}
		if s.Name == skillAlpha && s.Status == app.LockSkillFailed {
			t.Errorf("re-run alpha failed: %v", s.Err)
		}
	}
}

// TestInstallFromLock_UnsupportedSourceType (FR-030): a clear per-skill error.
func TestInstallFromLock_UnsupportedSourceType(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lock := `{
  "version": 1,
  "skills": {
    "npm-thing": {
      "source": "some-pkg",
      "sourceType": "node_modules",
      "skillPath": "SKILL.md",
      "computedHash": "` + strings.Repeat("1", 64) + `"
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := runLockInstall(t, root)
	if err == nil {
		t.Fatal("want error for unsupported sourceType")
	}
	if len(res.Skills) != 1 || res.Skills[0].Status != app.LockSkillFailed {
		t.Fatalf("Skills = %+v, want one failed", res.Skills)
	}
	if !strings.Contains(res.Skills[0].Err.Error(), "node_modules") {
		t.Errorf("error %v should name the unsupported type", res.Skills[0].Err)
	}
}

// TestInstallFromLock_MissingLock: clear failure when there is nothing to do.
func TestInstallFromLock_MissingLock(t *testing.T) {
	t.Parallel()
	if _, err := runLockInstall(t, t.TempDir()); err == nil {
		t.Fatal("want error when no skills-lock.json exists")
	} else if !strings.Contains(err.Error(), skillslock.FileName) {
		t.Errorf("error %v should name the missing file", err)
	}
}

// ---- US2: non-interactive behavior matrix (T025, research R4) ----------------

// TestInstallFromLock_NoAgentsAnywhere: nothing selected, nothing recorded →
// fail fast with usage guidance (FR-014), never prompt.
func TestInstallFromLock_NoAgentsAnywhere(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	_, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{Root: root})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Errorf("error %v should mention agents", err)
	}
}

func TestInstallFromLock_UnknownAgent(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	_, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root:   root,
		Agents: []string{"not-an-agent"},
	})
	if !errors.Is(err, errs.ErrUnsupportedAgent) {
		t.Fatalf("err = %v, want ErrUnsupportedAgent", err)
	}
}

// ---- US2: frozen and force semantics (T027, FR-018/FR-018a) ------------------

// TestInstallFromLock_FrozenNeverWritesLock: a frozen restore leaves the lock
// byte-identical (SC-007) while still materializing content.
func TestInstallFromLock_FrozenNeverWritesLock(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	// Enrich first: frozen refuses raw entries (it cannot write the gskill
	// block), so the frozen restore runs against an enriched lock.
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("enrich install: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "."+testAgent)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}

	res, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root:   root,
		Agents: []string{testAgent},
		Frozen: true,
	})
	if err != nil {
		t.Fatalf("InstallFromLock frozen: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("frozen run modified the lock:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if len(res.Skills) != 2 {
		t.Fatalf("Skills = %+v", res.Skills)
	}
	assertAgentTargets(t, root, "alpha", "beta")
}

// TestInstallFromLock_FrozenFailsClosedOnMismatch: hash drift under frozen
// aborts that skill and never rewrites the lock — even with Force set.
func TestInstallFromLock_FrozenFailsClosedOnMismatch(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	// Enrich first (frozen refuses raw entries), then corrupt beta's recorded
	// hash and remove its target so the frozen run must re-verify it.
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("enrich install: %v", err)
	}
	lockPath := filepath.Join(root, skillslock.FileName)
	enriched, err := os.ReadFile(lockPath) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(enriched), hb, strings.Repeat("0", 64), 1)
	if tampered == string(enriched) {
		t.Fatal("failed to tamper beta's computedHash")
	}
	if err := os.WriteFile(lockPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "."+testAgent, "skills", "beta")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(lockPath) //nolint:gosec // test-controlled temp path

	_, err = lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root:   root,
		Agents: []string{testAgent},
		Frozen: true,
		Force:  true, // force must be inert under frozen
	})
	if !errors.Is(err, errs.ErrPartialInstall) && !errors.Is(err, errs.ErrIntegrity) {
		t.Fatalf("err = %v, want integrity/partial failure", err)
	}
	after, _ := os.ReadFile(lockPath) //nolint:gosec // test-controlled temp path
	if string(before) != string(after) {
		t.Errorf("frozen failure modified the lock")
	}
	if _, statErr := os.Stat(filepath.Join(root, "."+testAgent, "skills", "beta")); statErr == nil {
		t.Error("mismatched skill was activated under frozen")
	}
}

// TestInstallFromLock_ForceAcceptsChangedContent (FR-018a): --force is the
// only way to accept changed upstream content; it rewrites computedHash and
// the gskill block.
func TestInstallFromLock_ForceAcceptsChangedContent(t *testing.T) {
	t.Parallel()
	repo, ha, _ := lockRepo(t)
	root := t.TempDir()
	stale := strings.Repeat("0", 64)
	writeLockOnly(t, root, repo, ha, stale) // beta records a stale hash

	res, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root:   root,
		Agents: []string{testAgent},
		Force:  true,
	})
	if err != nil {
		t.Fatalf("InstallFromLock --force: %v", err)
	}
	for _, s := range res.Skills {
		if s.Status != app.LockSkillInstalled {
			t.Errorf("%s status = %q (%v)", s.Name, s.Status, s.Err)
		}
	}
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := l.Entry("beta")
	if e.ComputedHash == stale || len(e.ComputedHash) != 64 {
		t.Errorf("beta computedHash = %q, want rewritten to actual content hash", e.ComputedHash)
	}
	if e.Ext == nil {
		t.Error("beta gskill block missing after force install")
	}
}

// ---- US5: idempotency, repair, offline (T042/T043, FR-017) -------------------

// TestInstallFromLock_SecondRunIsNoOp (SC-005): everything installed and
// matching → no work, lock byte-identical, success.
func TestInstallFromLock_SecondRunIsNoOp(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("first install: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}

	res, err := runLockInstall(t, root)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Changed {
		t.Error("Changed = true on a no-op re-install")
	}
	for _, s := range res.Skills {
		if s.Status != app.LockSkillUpToDate {
			t.Errorf("%s status = %q, want up-to-date", s.Name, s.Status)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if string(before) != string(after) {
		t.Errorf("no-op re-install changed the lock:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// TestInstallFromLock_RepairsMissingLink (US5): a deleted agent target is
// recreated from the store without touching anything else.
func TestInstallFromLock_RepairsMissingLink(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "."+testAgent, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}

	res, err := runLockInstall(t, root)
	if err != nil {
		t.Fatalf("repair run: %v", err)
	}
	statuses := map[string]string{}
	for _, s := range res.Skills {
		statuses[s.Name] = s.Status
	}
	if statuses[skillAlpha] != app.LockSkillRepaired {
		t.Errorf("alpha status = %q, want repaired", statuses[skillAlpha])
	}
	if statuses["beta"] != app.LockSkillUpToDate {
		t.Errorf("beta status = %q, want up-to-date", statuses["beta"])
	}
	assertAgentTargets(t, root, "alpha", "beta")
}

// TestInstallFromLock_OfflineRestoresFromStore (US5): with the store
// populated, --offline restores without any source access; with an empty
// store it fails with a source-unavailable diagnostic.
func TestInstallFromLock_OfflineRestoresFromStore(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Wipe the agent layer, keep the store; offline must restore it.
	if err := os.RemoveAll(filepath.Join(root, "."+testAgent)); err != nil {
		t.Fatal(err)
	}
	if _, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Offline: true,
	}); err != nil {
		t.Fatalf("offline restore: %v", err)
	}
	assertAgentTargets(t, root, "alpha", "beta")
}

// TestInstallFromLock_OfflineEmptyStoreFails (US5): nothing cached and no
// network allowed → clear failure.
func TestInstallFromLock_OfflineEmptyStoreFails(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	_, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Offline: true,
	})
	if err == nil {
		t.Fatal("offline install with an empty store should fail")
	}
}

// TestInstallFromLock_EditedHashOnInstalledProject (regression, quickstart
// S5): editing computedHash on an already-installed project must not be
// swallowed by the idempotency fast path — the default fails closed and
// --force rewrites the record.
func TestInstallFromLock_EditedHashOnInstalledProject(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Corrupt alpha's recorded hash in place (as a bad merge or edit would).
	lockPath := filepath.Join(root, skillslock.FileName)
	raw, err := os.ReadFile(lockPath) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	corrupted := strings.Replace(string(raw), ha, strings.Repeat("0", 64), 1)
	if corrupted == string(raw) {
		t.Fatal("fixture: hash not found to corrupt")
	}
	if err := os.WriteFile(lockPath, []byte(corrupted), 0o600); err != nil {
		t.Fatal(err)
	}

	// Default: fail closed, never report up-to-date.
	res, err := runLockInstall(t, root)
	if err == nil {
		t.Fatal("edited hash must not pass silently")
	}
	for _, s := range res.Skills {
		if s.Name == skillAlpha && s.Status == app.LockSkillUpToDate {
			t.Error("alpha reported up-to-date despite an edited recorded hash")
		}
	}

	// --force: accept and rewrite the record to the actual content hash.
	if _, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Force: true,
	}); err != nil {
		t.Fatalf("force install: %v", err)
	}
	l, err := skillslock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := l.Entry(skillAlpha)
	if e.ComputedHash != ha {
		t.Errorf("computedHash = %q, want rewritten back to actual %q", e.ComputedHash, ha)
	}
}

// skillAlpha is the first fixture skill's name.
const skillAlpha = "alpha"

// TestInstallFromLock_ExternalUpdateRefetches (review fix): when an external
// tool updates a skill (new content + computedHash) but the gskill block still
// pins the old commit, install must re-resolve and fetch the NEW content — not
// reinstall the stale pin and demand --force.
func TestInstallFromLock_ExternalUpdateRefetches(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("first install: %v", err)
	}

	newHash := simulateExternalUpdate(t, repo, root)
	lockPath := filepath.Join(root, skillslock.FileName)

	// No --force needed: the entry changed, so the stale pin must not be reused.
	res, err := runLockInstall(t, root)
	if err != nil {
		for _, s := range res.Skills {
			t.Logf("skill %s: status=%s err=%v", s.Name, s.Status, s.Err)
		}
		t.Fatalf("install after external update: %v", err)
	}
	for _, s := range res.Skills {
		if s.Name == skillAlpha && s.Status == app.LockSkillFailed {
			t.Fatalf("alpha failed instead of refetching: %v", s.Err)
		}
	}
	l2, err := skillslock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	e2, _ := l2.Entry(skillAlpha)
	if e2.ComputedHash != newHash {
		t.Errorf("computedHash = %q, want the external tool's %q preserved", e2.ComputedHash, newHash)
	}
	data, err := os.ReadFile(filepath.Join(root, "."+testAgent, "skills", skillAlpha, "SKILL.md")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "alpha v2") {
		t.Errorf("installed content is stale:\n%s", data)
	}
}

// gitCommit commits all changes in repo.
func gitCommit(t *testing.T, repo, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"commit", "--quiet", "-m", msg}} {
		cmd := exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // test-controlled fixture args
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// simulateExternalUpdate plays the external tool's role: commit new upstream
// content for alpha and record its new computedHash in the lock, leaving the
// gskill block (old pins) untouched.
func simulateExternalUpdate(t *testing.T, repo, root string) (newHash string) {
	t.Helper()
	newBody := "---\nname: alpha\ndescription: The alpha skill v2\n---\n# alpha v2\n"
	if err := os.WriteFile(filepath.Join(repo, "skills", "alpha", "SKILL.md"), []byte(newBody), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, repo, "alpha v2")
	var err error
	if newHash, err = integrity.CompatHash(filepath.Join(repo, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, skillslock.FileName)
	l, err := skillslock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := l.Entry(skillAlpha)
	l.SetEntry(skillAlpha, skillslock.Entry{Source: e.Source, SourceType: e.SourceType, SkillPath: e.SkillPath, ComputedHash: newHash})
	if err := skillslock.Save(lockPath, l); err != nil {
		t.Fatal(err)
	}
	return newHash
}

// TestInstallAgentUnionPersists: --agent on an enriched lock is a union — the
// new agent is added to each skill's persisted gskill.agents and existing
// agents keep their installs (nothing is uninstalled by an install).
func TestInstallAgentUnionPersists(t *testing.T) {
	t.Parallel()
	repo, _, _ := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, mustCompat(t, repo, "alpha"), mustCompat(t, repo, "beta"))
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("enrich install: %v", err)
	}

	res, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{"codex"},
	})
	if err != nil {
		t.Fatalf("union install: %v", err)
	}
	_ = res
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		e, _ := l.Entry(name)
		if e.Ext == nil || len(e.Ext.Agents) != 2 || e.Ext.Agents[0] != testAgent || e.Ext.Agents[1] != "codex" {
			t.Errorf("%s agents = %+v, want [claude codex]", name, e.Ext)
		}
		for _, marker := range []string{"." + testAgent, ".codex"} {
			if _, statErr := os.Stat(filepath.Join(root, marker, "skills", name)); statErr != nil {
				t.Errorf("%s target for %s missing: %v", name, marker, statErr)
			}
		}
	}
}

// mustCompat hashes one skill dir of the fixture repo.
func mustCompat(t *testing.T, repo, name string) string {
	t.Helper()
	h, err := integrity.CompatHash(filepath.Join(repo, "skills", name))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestInstallFromLock_UnmanagedSameNameFailsWithoutForce (§13): hand-written
// content already sitting at an agent target is never silently replaced; the
// skill fails closed until --force approves the overwrite.
func TestInstallFromLock_UnmanagedSameNameFailsWithoutForce(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)

	foreign := filepath.Join(root, "."+testAgent, "skills", "alpha")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	body := []byte("---\nname: alpha\ndescription: hand-written\n---\n# mine\n")
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := runLockInstall(t, root)
	if err == nil {
		t.Fatal("install over unmanaged same-name content succeeded, want fail-closed")
	}
	for _, s := range res.Skills {
		if s.Name == "alpha" && s.Status != app.LockSkillFailed {
			t.Errorf("alpha status = %q, want failed", s.Status)
		}
	}
	got, err := os.ReadFile(filepath.Join(foreign, "SKILL.md")) //nolint:gosec // test-controlled temp path
	if err != nil || string(got) != string(body) {
		t.Fatalf("unmanaged content changed: %v %q", err, got)
	}

	// --force approves the replacement.
	if _, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Force: true,
	}); err != nil {
		t.Fatalf("forced install: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(foreign, "SKILL.md")) //nolint:gosec // test-controlled temp path
	if err != nil || string(got) == string(body) {
		t.Fatal("forced install did not replace the unmanaged content")
	}
}
