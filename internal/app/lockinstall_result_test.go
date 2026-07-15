package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// resultByName pulls one skill's result out of a run.
func resultByName(t *testing.T, res app.InstallFromLockResult, name string) app.LockSkillResult {
	t.Helper()
	for _, r := range res.Skills {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no result for skill %q in %+v", name, res.Skills)
	return app.LockSkillResult{}
}

// TestLockResult_SuccessProvenance (FR-011): successful entries carry their
// provenance so every renderer can display it without re-deriving.
func TestLockResult_SuccessProvenance(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)

	res, err := runLockInstall(t, root)
	if err != nil {
		t.Fatalf("InstallFromLock: %v", err)
	}
	r := resultByName(t, res, "alpha")
	if r.Status != app.LockSkillInstalled {
		t.Fatalf("alpha status = %q, want installed", r.Status)
	}
	if r.SourceType != "local" {
		t.Errorf("SourceType = %q, want local", r.SourceType)
	}
	if r.SkillPath != "skills/alpha/SKILL.md" {
		t.Errorf("SkillPath = %q, want the lock entry's skillPath", r.SkillPath)
	}
	if len(r.Agents) != 1 || r.Agents[0] != testAgent {
		t.Errorf("Agents = %v, want [%s]", r.Agents, testAgent)
	}
	if r.InstallMode == "" {
		t.Error("InstallMode empty on a successful install")
	}
	if r.Phase != app.InstallPhaseComplete {
		t.Errorf("Phase = %q, want complete", r.Phase)
	}
	if r.Failure != nil {
		t.Errorf("Failure = %+v on a successful install, want nil", r.Failure)
	}
	if r.Commit == "" {
		t.Error("Commit empty for a git-resolved local source")
	}
}

// TestLockResult_IntegrityMismatchMetadata: the fail-closed integrity path
// carries category, phase, message, hint, and the expected/actual hashes —
// while sibling skills stay isolated (FR-016a).
func TestLockResult_IntegrityMismatchMetadata(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	_ = hashAlpha
	corrupted := "sha256:" + strings.Repeat("0", 64)
	root := t.TempDir()
	writeLockOnly(t, root, repo, corrupted, hashBeta)

	res, err := runLockInstall(t, root)
	if err == nil {
		t.Fatal("InstallFromLock succeeded despite a computedHash mismatch")
	}
	r := resultByName(t, res, "alpha")
	if r.Status != app.LockSkillFailed {
		t.Fatalf("alpha status = %q, want failed", r.Status)
	}
	if r.Phase != app.InstallPhaseVerifying {
		t.Errorf("Phase = %q, want verifying", r.Phase)
	}
	if r.Failure == nil {
		t.Fatal("Failure nil on a failed entry (FR-011)")
	}
	if r.Failure.Category != app.FailureIntegrity {
		t.Errorf("Category = %q, want integrity", r.Failure.Category)
	}
	if r.Failure.Expected != corrupted {
		t.Errorf("Expected = %q, want the recorded hash %q", r.Failure.Expected, corrupted)
	}
	if r.Failure.Actual == "" || r.Failure.Actual == corrupted {
		t.Errorf("Actual = %q, want the real computed hash", r.Failure.Actual)
	}
	if !strings.Contains(r.Failure.Hint, "--force") {
		t.Errorf("Hint = %q, want the --force remediation", r.Failure.Hint)
	}
	if r.Failure.Message == "" {
		t.Error("Message empty on a failed entry")
	}
	if beta := resultByName(t, res, "beta"); beta.Status != app.LockSkillInstalled {
		t.Errorf("beta status = %q, want installed (failure isolation)", beta.Status)
	}
}

// TestLockResult_UnsupportedSourceType: the one construction site is marked
// with a dedicated sentinel so classification never parses the message.
func TestLockResult_UnsupportedSourceType(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lock := `{
  "version": 1,
  "skills": {
    "alpha": {
      "source": "ftp://example.com/skills",
      "sourceType": "ftp",
      "skillPath": "skills/alpha/SKILL.md",
      "computedHash": "sha256:` + strings.Repeat("1", 64) + `"
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := runLockInstall(t, root)
	if err == nil {
		t.Fatal("InstallFromLock succeeded despite an unsupported sourceType")
	}
	r := resultByName(t, res, "alpha")
	if r.Failure == nil || r.Failure.Category != app.FailureUnsupportedSource {
		t.Errorf("Failure = %+v, want category unsupported-source", r.Failure)
	}
}

// TestLockResult_SkillPathNotFound: a metadata problem classifies as
// invalid-metadata at the reading-metadata phase.
func TestLockResult_SkillPathNotFound(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, _ := lockRepo(t)
	root := t.TempDir()
	lock := `{
  "version": 1,
  "skills": {
    "ghost": {
      "source": ` + jsonStr(repo) + `,
      "sourceType": "local",
      "skillPath": "skills/ghost/SKILL.md",
      "computedHash": "` + hashAlpha + `"
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := runLockInstall(t, root)
	if err == nil {
		t.Fatal("InstallFromLock succeeded despite a missing skillPath")
	}
	r := resultByName(t, res, "ghost")
	if r.Failure == nil {
		t.Fatal("Failure nil on a failed entry")
	}
	if r.Failure.Category != app.FailureInvalidMetadata {
		t.Errorf("Category = %q, want invalid-metadata", r.Failure.Category)
	}
	if r.Phase != app.InstallPhaseReadingMetadata {
		t.Errorf("Phase = %q, want reading-metadata", r.Phase)
	}
}

// TestLockResult_ForeignContentMetadata: an unmanaged target fails closed and
// classifies as foreign-content at the linking phase.
func TestLockResult_ForeignContentMetadata(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)

	foreign := filepath.Join(root, "."+testAgent, "skills", "alpha")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("# not gskill's\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := runLockInstall(t, root)
	if err == nil {
		t.Fatal("InstallFromLock succeeded despite a foreign target")
	}
	r := resultByName(t, res, "alpha")
	if r.Failure == nil {
		t.Fatal("Failure nil on a failed entry")
	}
	if r.Failure.Category != app.FailureForeignContent {
		t.Errorf("Category = %q, want foreign-content", r.Failure.Category)
	}
	if r.Phase != app.InstallPhaseLinking {
		t.Errorf("Phase = %q, want linking", r.Phase)
	}
}

// snapshotTree captures path -> content for every regular file under root,
// excluding runtime scratch state FR-026 does not protect: the flock file
// under .gskill/locks (withLock creates it on every run, dry or not) and the
// download cache under .gskill/cache (planning must fetch content to verify
// hashes; the cache is not installed state). The store, agent targets, and
// skills-lock.json — the managed state the invariant is about — all remain
// in the snapshot.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr // absent trees snapshot as empty
		}
		if strings.Contains(path, filepath.Join(".gskill", "locks")) ||
			strings.Contains(path, filepath.Join(".gskill", "cache")) {
			return nil
		}
		b, rErr := os.ReadFile(path) //nolint:gosec // test-controlled temp path
		if rErr == nil {
			snap[path] = string(b)
		}
		return nil
	})
	return snap
}

// snapshotDiff reports the paths added, removed, or modified between two
// snapshots ("" means identical).
func snapshotDiff(before, after map[string]string) string {
	var diffs []string
	for k, v := range after {
		if bv, ok := before[k]; !ok {
			diffs = append(diffs, "added: "+k)
		} else if bv != v {
			diffs = append(diffs, "modified: "+k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			diffs = append(diffs, "removed: "+k)
		}
	}
	return strings.Join(diffs, "\n")
}

// dryRunInstall runs a dry-run InstallFromLock (FR-026 helpers below assert
// its planned actions and no-mutation invariant).
func dryRunInstall(t *testing.T, root string, agents []string) app.InstallFromLockResult {
	t.Helper()
	res, _ := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: agents, DryRun: true,
	})
	return res
}

// seededLockProject builds a lock-only project and installs it once.
func seededLockProject(t *testing.T) string {
	t.Helper()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("seed install: %v", err)
	}
	return root
}

// TestLockResult_DryRunWouldInstall (FR-026): a fresh entry plans
// would-install, and the dry run mutates nothing.
func TestLockResult_DryRunWouldInstall(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)
	before := snapshotTree(t, root)

	res := dryRunInstall(t, root, []string{testAgent})
	r := resultByName(t, res, "alpha")
	if r.Status != app.LockSkillPlanned || r.PlannedAction != app.PlannedWouldInstall {
		t.Errorf("alpha = %q/%q, want planned/would-install", r.Status, r.PlannedAction)
	}
	if diff := snapshotDiff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("dry run mutated the project tree (FR-026):\n%s", diff)
	}
}

// TestLockResult_DryRunWouldUpdateLock: widening the agent set on a recorded
// entry plans a lock rewrite.
func TestLockResult_DryRunWouldUpdateLock(t *testing.T) {
	t.Parallel()
	root := seededLockProject(t)
	res := dryRunInstall(t, root, []string{testAgent, "codex"})
	r := resultByName(t, res, "alpha")
	if r.Status != app.LockSkillPlanned || r.PlannedAction != app.PlannedWouldUpdateLock {
		t.Errorf("alpha = %q/%q, want planned/would-update-lock", r.Status, r.PlannedAction)
	}
}

// TestLockResult_DryRunWouldRepair: a missing managed target plans a relink.
func TestLockResult_DryRunWouldRepair(t *testing.T) {
	t.Parallel()
	root := seededLockProject(t)
	if err := os.RemoveAll(filepath.Join(root, "."+testAgent, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	res := dryRunInstall(t, root, []string{testAgent})
	r := resultByName(t, res, "alpha")
	if r.Status != app.LockSkillPlanned || r.PlannedAction != app.PlannedWouldRepair {
		t.Errorf("alpha = %q/%q, want planned/would-repair", r.Status, r.PlannedAction)
	}
}

// TestLockResult_DryRunWouldRemoveTarget: an explicit empty selection plans
// target removal (spec 013 narrow-to-zero).
func TestLockResult_DryRunWouldRemoveTarget(t *testing.T) {
	t.Parallel()
	root := seededLockProject(t)
	res := dryRunInstall(t, root, []string{})
	r := resultByName(t, res, "alpha")
	if r.Status != app.LockSkillPlanned || r.PlannedAction != app.PlannedWouldRemoveTarget {
		t.Errorf("alpha = %q/%q, want planned/would-remove-target", r.Status, r.PlannedAction)
	}
}

// TestLockResult_DryRunBlocked: a plan that would fail reports blocked.
func TestLockResult_DryRunBlocked(t *testing.T) {
	t.Parallel()
	repo, _, hashBeta := lockRepo(t)
	corrupted := "sha256:" + strings.Repeat("0", 64)
	root := t.TempDir()
	writeLockOnly(t, root, repo, corrupted, hashBeta)
	res := dryRunInstall(t, root, []string{testAgent})
	r := resultByName(t, res, "alpha")
	if r.Status != app.LockSkillFailed || r.PlannedAction != app.PlannedBlocked {
		t.Errorf("alpha = %q/%q, want failed/blocked", r.Status, r.PlannedAction)
	}
}
