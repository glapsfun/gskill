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

// coexistProject builds a fully installed project whose skills-lock.json also
// carries foreign data gskill does not own: an unknown top-level field, an
// unknown entry field, another tool's per-entry block, and one external-only
// entry gskill never installed.
func coexistProject(t *testing.T) (root string) {
	t.Helper()
	repo, ha, hb := lockRepo(t)
	root = t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatalf("setup install: %v", err)
	}

	// Seed an external-only entry after the install (as npx skills would).
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	l.SetEntry("external-only", skillslock.Entry{
		Source:       "acme/elsewhere",
		SourceType:   "github",
		SkillPath:    "skills/external-only/SKILL.md",
		ComputedHash: strings.Repeat("9", 64),
	})
	if err := skillslock.Save(filepath.Join(root, skillslock.FileName), l); err != nil {
		t.Fatal(err)
	}
	return root
}

// assertForeignSurvives checks every piece of foreign data after an operation.
func assertForeignSurvives(t *testing.T, root, op string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("%s: read lock: %v", op, err)
	}
	s := string(raw)
	for _, want := range []string{
		`"customTopLevel": "keep-me"`,
		`"otherTool": {`,
		`"external-only"`,
		`"acme/elsewhere"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("%s lost foreign data %q:\n%s", op, want, s)
		}
	}
	// The file must remain a valid v1 document for external consumers.
	if _, err := skillslock.Unmarshal(raw); err != nil {
		t.Errorf("%s left an invalid shared lock: %v", op, err)
	}
}

// TestCoexist_LockTouchingCommandsPreserveForeignData (T038/SC-002): every
// lock-rewriting command keeps unknown fields and external entries intact.
func TestCoexist_LockTouchingCommandsPreserveForeignData(t *testing.T) {
	t.Parallel()
	ops := []struct {
		name string
		run  func(a *app.App, root string) error
	}{
		{"update", func(a *app.App, root string) error {
			_, err := a.Update(context.Background(), root, nil)
			return err
		}},
		{"sync", func(a *app.App, root string) error {
			_, err := a.Sync(context.Background(), app.SyncRequest{Root: root})
			return err
		}},
		{"reinstall", func(a *app.App, root string) error {
			// The foreign external-only entry's fake source cannot install;
			// per-skill isolation reports it as a partial failure while the
			// preservation guarantee under test still holds.
			_, err := a.InstallFromLock(context.Background(),
				app.InstallFromLockRequest{Root: root, Agents: []string{"claude"}})
			if errors.Is(err, errs.ErrPartialInstall) {
				return nil
			}
			return err
		}},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()
			root := coexistProject(t)
			if err := op.run(lockApp(), root); err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
			assertForeignSurvives(t, root, op.name)
		})
	}
}

// TestCoexist_RemoveDeletesOnlyThatEntry (T039/FR-006): removing a skill drops
// exactly its entry; foreign data and unrelated entries stay byte-identical.
func TestCoexist_RemoveDeletesOnlyThatEntry(t *testing.T) {
	t.Parallel()
	root := coexistProject(t)

	if _, err := lockApp().Remove(context.Background(), root, []string{"alpha"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	s := string(raw)
	if strings.Contains(s, `"alpha"`) {
		t.Errorf("removed entry still present:\n%s", s)
	}
	for _, want := range []string{`"beta"`, `"external-only"`, `"customTopLevel": "keep-me"`} {
		if !strings.Contains(s, want) {
			t.Errorf("remove lost unrelated data %q:\n%s", want, s)
		}
	}
}

// TestCoexist_FailClosedOnUnparsableLock (T039): a rewrite must never clobber
// a shared file gskill cannot parse — the corrupt bytes stay untouched.
func TestCoexist_FailClosedOnUnparsableLock(t *testing.T) {
	t.Parallel()
	root := coexistProject(t)
	corrupt := []byte("{ this is not json")
	lockPath := filepath.Join(root, skillslock.FileName)
	if err := os.WriteFile(lockPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := lockApp().Update(context.Background(), root, nil); err == nil {
		t.Fatal("Update on a corrupt shared lock should fail")
	}
	after, _ := os.ReadFile(lockPath) //nolint:gosec // test-controlled temp path
	if string(after) != string(corrupt) {
		t.Errorf("corrupt lock was rewritten: %q", after)
	}
}

// TestCoexist_SecondInstallKeepsLockValidForExternalTools (SC-008): after
// repeated gskill operations the minimal v1 core of every entry still parses
// and validates.
func TestCoexist_SecondInstallKeepsLockValidForExternalTools(t *testing.T) {
	t.Parallel()
	root := coexistProject(t)
	if _, err := runLockInstall(t, root); err == nil {
		// The external-only entry has an unreachable source, so a full
		// re-install reports a partial failure — that must not corrupt the
		// file for external consumers.
		t.Log("second install unexpectedly clean (external entry resolved?)")
	}
	assertForeignSurvives(t, root, "second install")
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatalf("lock unreadable after second install: %v", err)
	}
	if err := l.Validate(); err != nil {
		t.Errorf("lock invalid after second install: %v", err)
	}
}
