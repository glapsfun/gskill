package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// legacyProject builds a project holding gskill.toml + a populated gskill.lock
// and no skills-lock.json (a pre-012 checkout).
func legacyProject(t *testing.T) (root string, legacyBytes []byte) {
	t.Helper()
	root = t.TempDir()
	m := manifest.New()
	m.Skills["demo"] = manifest.Skill{Source: "github.com/acme/skills", Path: "skills/demo"}
	if err := manifest.Save(filepath.Join(root, "gskill.toml"), m); err != nil {
		t.Fatal(err)
	}
	lf := lockfile.New()
	lf.Skills["demo"] = lockfile.LockedSkill{
		Source: lockfile.Source{
			Type: "github", Original: "acme/skills", Owner: "acme", Repo: "skills", Path: "skills/demo",
			URL: "https://github.com/acme/skills.git",
		},
		Resolved: lockfile.Resolved{
			RefKind: "branch", Branch: "main", Commit: "cafe1234",
			ContentHash: "sha256:5555555555555555555555555555555555555555555555555555555555555555",
		},
		Metadata:     lockfile.Metadata{Name: "demo", Description: "Demo"},
		Installation: lockfile.Installation{Scope: "project", Mode: "symlink", Agents: []string{"claude"}},
	}
	if err := lockfile.Save(filepath.Join(root, "gskill.lock"), lf); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "gskill.lock")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	return root, data
}

// TestMigrateLockfile_ConvertsBacksUpAndRetires (T033/FR-008..FR-009): the
// explicit migration writes the shared lock, keeps a byte-identical backup,
// and deletes the legacy file only afterwards.
func TestMigrateLockfile_ConvertsBacksUpAndRetires(t *testing.T) {
	t.Parallel()
	root, legacyBytes := legacyProject(t)

	res, err := lockApp().MigrateLockfile(context.Background(), root)
	if err != nil {
		t.Fatalf("MigrateLockfile: %v", err)
	}
	if !res.Migrated || res.AlreadyMigrated {
		t.Errorf("res = %+v, want Migrated", res)
	}
	assertMigratedEntry(t, root)
	assertBackupAndRetirement(t, root, legacyBytes)
}

// assertMigratedEntry verifies the converted shared-lock entry.
func assertMigratedEntry(t *testing.T, root string) {
	t.Helper()
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatalf("shared lock unreadable: %v", err)
	}
	e, ok := l.Entry("demo")
	if !ok || e.Ext == nil {
		t.Fatalf("demo entry not migrated: %+v", e)
	}
	if e.Source != "acme/skills" || e.SkillPath != "skills/demo/SKILL.md" {
		t.Errorf("core fields = %+v", e)
	}
	if e.Ext.Commit != "cafe1234" || e.Ext.StoreHash == "" {
		t.Errorf("gskill block = %+v", e.Ext)
	}
}

// assertBackupAndRetirement verifies FR-009's crash-safe artifacts.
func assertBackupAndRetirement(t *testing.T, root string, legacyBytes []byte) {
	t.Helper()
	backup, err := os.ReadFile(filepath.Join(root, "gskill.lock.backup")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(backup) != string(legacyBytes) {
		t.Error("backup is not byte-identical to the original gskill.lock")
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.lock")); err == nil {
		t.Error("gskill.lock still present after migration")
	}
}

// TestMigrateLockfile_BothFilesMergesIntoCanonical (T033/FR-011): with both
// files present, skills-lock.json stays canonical, gains missing gskill
// metadata, keeps foreign data, and the legacy file is backed up and retired.
func TestMigrateLockfile_BothFilesMergesIntoCanonical(t *testing.T) {
	t.Parallel()
	root, _ := legacyProject(t)
	shared := `{
  "version": 1,
  "keepTop": true,
  "skills": {
    "demo": {
      "source": "acme/skills",
      "sourceType": "github",
      "skillPath": "skills/demo/SKILL.md",
      "computedHash": "` + strings.Repeat("6", 64) + `",
      "otherTool": {"x": 1}
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := lockApp().MigrateLockfile(context.Background(), root)
	if err != nil {
		t.Fatalf("MigrateLockfile: %v", err)
	}
	if !res.Migrated {
		t.Errorf("res = %+v, want Migrated (metadata merge)", res)
	}

	raw, _ := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	s := string(raw)
	for _, want := range []string{
		`"keepTop": true`,
		`"otherTool": {`,
		`"computedHash": "` + strings.Repeat("6", 64) + `"`, // canonical value kept
		`"gskill": {`, // legacy metadata merged in
		`"cafe1234"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("canonical lock missing %q:\n%s", want, s)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.lock")); err == nil {
		t.Error("gskill.lock still present")
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.lock.backup")); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}

// TestMigrateLockfile_NothingToMigrate: neither file → clear error; only the
// shared lock → no-op success.
func TestMigrateLockfile_NothingToMigrate(t *testing.T) {
	t.Parallel()
	if _, err := lockApp().MigrateLockfile(context.Background(), t.TempDir()); err == nil {
		t.Error("want error when neither lock exists")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName),
		[]byte(`{"version": 1, "skills": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := lockApp().MigrateLockfile(context.Background(), root)
	if err != nil {
		t.Fatalf("MigrateLockfile on migrated project: %v", err)
	}
	if !res.AlreadyMigrated || res.Migrated {
		t.Errorf("res = %+v, want AlreadyMigrated no-op", res)
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.lock.backup")); err == nil {
		t.Error("no-op migration created a backup")
	}
}

// TestMigrate_AutoTriggerOnLockTouchingCommands (T034/FR-010): install,
// update, project lock, and project sync migrate a legacy-only project before
// proceeding, and never recreate gskill.lock.
func TestMigrate_AutoTriggerOnLockTouchingCommands(t *testing.T) {
	t.Parallel()
	ops := []struct {
		name string
		run  func(a *app.App, root string) error
	}{
		{"install", func(a *app.App, root string) error {
			_, err := a.Install(context.Background(), app.InstallRequest{Root: root})
			return err
		}},
		{"update", func(a *app.App, root string) error {
			_, err := a.Update(context.Background(), root, nil)
			return err
		}},
		{"lock", func(a *app.App, root string) error {
			_, err := a.Lock(context.Background(), root)
			return err
		}},
		{"sync", func(a *app.App, root string) error {
			_, err := a.Sync(context.Background(), app.SyncRequest{Root: root})
			return err
		}},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()
			root, _ := legacyProject(t)
			// The operation itself may fail (the fake source is unreachable);
			// the migration must have happened regardless.
			_ = op.run(lockApp(), root)

			if _, err := os.Stat(filepath.Join(root, skillslock.FileName)); err != nil {
				t.Errorf("skills-lock.json missing after %s: %v", op.name, err)
			}
			if _, err := os.Stat(filepath.Join(root, "gskill.lock")); err == nil {
				t.Errorf("gskill.lock still present after %s", op.name)
			}
			if _, err := os.Stat(filepath.Join(root, "gskill.lock.backup")); err != nil {
				t.Errorf("backup missing after %s: %v", op.name, err)
			}
		})
	}
}
