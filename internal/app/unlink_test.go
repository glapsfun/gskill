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

// TestInstallNarrowRefusesForeignModifiedCopyModeTarget (FR-011/FR-013):
// narrowing a copy-mode agent target whose on-disk content was hand-edited
// after install must fail safely rather than silently deleting it — and the
// check is unconditional: passing Force does not bypass it (research.md
// Decision 3, resolving the /speckit-analyze finding F1 self-contradiction).
func TestInstallNarrowRefusesForeignModifiedCopyModeTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"},
		Agents: []string{"claude", "codex"}, Mode: "copy",
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a hand-edit of codex's copy-mode target after install.
	target := filepath.Join(root, ".codex", "skills", "demo-skill", "SKILL.md")
	edited := "---\nname: demo-skill\ndescription: hand-edited\n---\n# mine\n"
	if err := os.WriteFile(target, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, force := range []bool{false, true} {
		res, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{
			Root: root, Agents: []string{"claude"}, Force: force,
		})
		if err == nil {
			t.Fatalf("force=%v: narrow over foreign-modified copy content succeeded, want fail-closed", force)
		}
		for _, s := range res.Skills {
			if s.Name == "demo-skill" && s.Status != app.LockSkillFailed {
				t.Errorf("force=%v: status = %q, want failed", force, s.Status)
			}
		}
		got, readErr := os.ReadFile(target) //nolint:gosec // test-controlled temp path
		if readErr != nil || !strings.Contains(string(got), "hand-edited") {
			t.Fatalf("force=%v: foreign content was removed: err=%v content=%q", force, readErr, got)
		}
		l, loadErr := skillslock.Load(filepath.Join(root, skillslock.FileName))
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		e, _ := l.Entry("demo-skill")
		if e.Ext == nil || len(e.Ext.Agents) != 2 {
			t.Errorf("force=%v: agents = %+v, want unchanged [claude codex]", force, e.Ext)
		}
	}
}

// TestUnlinkRefusesForeignModifiedCopyModeTarget (code-review fix): gskill
// unlink must enforce the same foreign-content ownership check as install
// narrowing — both remove an agent's target through the same primitive
// (checkSafeTargetRemoval), so a hand-edited copy-mode target is never
// silently deleted regardless of which command reaches it.
func TestUnlinkRefusesForeignModifiedCopyModeTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"},
		Agents: []string{"claude", "codex"}, Mode: "copy",
	}); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, ".codex", "skills", "demo-skill", "SKILL.md")
	edited := "---\nname: demo-skill\ndescription: hand-edited\n---\n# mine\n"
	if err := os.WriteFile(target, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := a.Unlink(ctx, root, "demo-skill", "codex", false)
	if err == nil {
		t.Fatal("unlink over foreign-modified copy content succeeded, want fail-closed")
	}
	if !errors.Is(err, errs.ErrInvalidLock) {
		t.Errorf("err = %v, want ErrInvalidLock", err)
	}
	got, readErr := os.ReadFile(target) //nolint:gosec // test-controlled temp path
	if readErr != nil || !strings.Contains(string(got), "hand-edited") {
		t.Fatalf("foreign content was removed: err=%v content=%q", readErr, got)
	}
	l, loadErr := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	e, _ := l.Entry("demo-skill")
	if e.Ext == nil || len(e.Ext.Agents) != 2 {
		t.Errorf("agents = %+v, want unchanged [claude codex]", e.Ext)
	}
}

// TestInstallNarrowPartialFailureLeavesLockAndDiskUntouched (code-review
// fix): when narrowing drops multiple agents and one target fails its
// ownership check, the whole batch must abort before any removal — not
// remove the agents that would have succeeded and leave the lock
// inconsistent with disk.
func TestInstallNarrowPartialFailureLeavesLockAndDiskUntouched(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"},
		Agents: []string{"claude", "codex", "cursor"}, Mode: "copy",
	}); err != nil {
		t.Fatal(err)
	}

	// codex's copy-mode target is hand-edited; cursor's is untouched.
	// Narrowing to claude only tries to remove both.
	codexTarget := filepath.Join(root, ".codex", "skills", "demo-skill", "SKILL.md")
	if err := os.WriteFile(codexTarget, []byte("---\nname: demo-skill\ndescription: hand-edited\n---\n# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root, Agents: []string{"claude"}})
	if err == nil {
		t.Fatal("narrow with a foreign-modified target among the removals succeeded, want fail-closed")
	}

	l, loadErr := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	e, _ := l.Entry("demo-skill")
	if e.Ext == nil || len(e.Ext.Agents) != 3 {
		t.Fatalf("agents = %+v, want unchanged [claude codex cursor] (whole batch aborted)", e.Ext)
	}
	// cursor's target must still be present on disk too — a single bad
	// target in the batch must not remove the other, clean ones.
	if _, statErr := os.Stat(filepath.Join(root, ".cursor", "skills", "demo-skill")); statErr != nil {
		t.Errorf("cursor's target was removed despite the batch failing closed: %v", statErr)
	}
	if _, statErr := os.Stat(codexTarget); statErr != nil {
		t.Errorf("codex's foreign-modified target was removed: %v", statErr)
	}
}
