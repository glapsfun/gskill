package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// A missing skills-lock.json must fail closed: reading it as "nothing
// declared" would let --prune wipe every managed install.
func TestSyncMissingLockFailsClosed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"}, Agents: []string{"claude"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, skillslock.FileName)); err != nil {
		t.Fatal(err)
	}

	_, err := a.Sync(ctx, app.SyncRequest{Root: root, Prune: true})
	if err == nil {
		t.Fatal("sync --prune with a missing lock succeeded, want fail-closed")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude", "skills", "demo-skill")); statErr != nil {
		t.Fatalf("managed install was touched despite the failure: %v", statErr)
	}
}

// Update and Remove outside a project fail with the missing-lock error
// instead of silently succeeding.
func TestUpdateRemoveMissingLockFail(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Update(ctx, root, nil); err == nil {
		t.Error("update without a lock succeeded, want error")
	}
	if _, err := a.Remove(ctx, root, []string{"x"}); err == nil {
		t.Error("remove without a lock succeeded, want error")
	}
}

// External-only entries stay untouched by prune: the lock still declares the
// skill even though another tool manages it.
func TestPrunePreservesExternalEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"}, Agents: []string{"claude"},
	}); err != nil {
		t.Fatal(err)
	}

	// Strip the gskill block, leaving a valid external-only declaration.
	lockPath := filepath.Join(root, skillslock.FileName)
	l, err := skillslock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := l.Entry("demo-skill")
	l.Remove("demo-skill")
	l.SetEntry("demo-skill", skillslock.Entry{
		Source: e.Source, SourceType: e.SourceType, SkillPath: e.SkillPath, ComputedHash: e.ComputedHash,
	})
	if err := skillslock.Save(lockPath, l); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Sync(ctx, app.SyncRequest{Root: root, Prune: true}); err != nil {
		t.Fatalf("sync --prune: %v", err)
	}
	for _, p := range []string{
		filepath.Join(root, ".claude", "skills", "demo-skill"),
		filepath.Join(root, ".agents", "skills", "demo-skill"),
	} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("still-declared external entry's install was pruned: %s: %v", p, statErr)
		}
	}
}

// An agent dropped from a still-declared entry's gskill.agents is pruned.
func TestPruneRemovesDroppedAgentTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"}, Agents: []string{"claude", "codex"},
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a teammate's unlink+commit: shrink the entry's agents/targets.
	lockPath := filepath.Join(root, skillslock.FileName)
	l, err := skillslock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := l.Entry("demo-skill")
	e.Ext.Agents = []string{"claude"}
	if e.Ext.State != nil {
		delete(e.Ext.State.Targets, "codex")
		delete(e.Ext.State.Modes, "codex")
	}
	l.SetEntry("demo-skill", e)
	if err := skillslock.Save(lockPath, l); err != nil {
		t.Fatal(err)
	}

	res, err := a.Sync(ctx, app.SyncRequest{Root: root, Prune: true})
	if err != nil {
		t.Fatalf("sync --prune: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".codex", "skills", "demo-skill")); !os.IsNotExist(statErr) {
		t.Errorf("dropped agent's target survived prune (err=%v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude", "skills", "demo-skill")); statErr != nil {
		t.Errorf("declared agent's target was pruned: %v", statErr)
	}
	if res.UpToDate {
		t.Error("a pruning sync reported up-to-date")
	}
}

// Adding a skill whose name collides with another tool's lock entry fails
// closed instead of corrupting that entry.
func TestAddRefusesExternalEntryCollision(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	lock := `{"version":1,"skills":{"demo-skill":{"source":"acme/elsewhere","sourceType":"github","skillPath":"skills/demo-skill/SKILL.md","computedHash":"abc"}}}`
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"}, Agents: []string{"claude"},
	})
	if err == nil {
		t.Fatal("add over another tool's entry succeeded, want conflict")
	}
	if !strings.Contains(err.Error(), "another tool") {
		t.Errorf("error = %v, want the external-collision wording", err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if !strings.Contains(string(raw), `"acme/elsewhere"`) || strings.Contains(string(raw), `"gskill"`) {
		t.Errorf("external entry was modified:\n%s", raw)
	}
}

// A managed entry whose every agent was unlinked (empty gskill.agents) must
// not block the rest of the install run.
func TestInstallSkipsAgentlessManagedEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill", "skills/other-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, All: true, Agents: []string{"claude"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Unlink(ctx, root, "demo-skill", "claude", false); err != nil {
		t.Fatal(err)
	}

	res, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root})
	if err != nil {
		t.Fatalf("install after unlink: %v", err)
	}
	byName := map[string]string{}
	for _, s := range res.Skills {
		byName[s.Name] = s.Status
	}
	if byName["demo-skill"] != app.LockSkillUpToDate {
		t.Errorf("agentless entry status = %q, want up-to-date skip", byName["demo-skill"])
	}
	if byName["other-skill"] == app.LockSkillFailed {
		t.Error("sibling entry failed")
	}
}

// A raw entry without agents fails per-skill, not the whole run: managed
// entries still restore (FR-016a).
func TestInstallRawEntryFailsPerSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"}, Agents: []string{"claude"},
	}); err != nil {
		t.Fatal(err)
	}
	// Hand-append a raw entry with an unreachable source and no agents.
	lockPath := filepath.Join(root, skillslock.FileName)
	l, err := skillslock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	l.SetEntry("foreign", skillslock.Entry{
		Source: "acme/elsewhere", SourceType: "github",
		SkillPath: "skills/foreign/SKILL.md", ComputedHash: "abc",
	})
	if err := skillslock.Save(lockPath, l); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}

	res, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root})
	if err == nil {
		t.Fatal("run with an unrestorable raw entry succeeded, want partial failure")
	}
	if !errors.Is(err, errs.ErrPartialInstall) {
		t.Errorf("err = %v, want partial-install class", err)
	}
	byName := map[string]string{}
	for _, s := range res.Skills {
		byName[s.Name] = s.Status
	}
	if byName["foreign"] != app.LockSkillFailed {
		t.Errorf("raw entry status = %q, want failed", byName["foreign"])
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude", "skills", "demo-skill")); statErr != nil {
		t.Errorf("managed entry was not restored despite per-skill isolation: %v", statErr)
	}
}
