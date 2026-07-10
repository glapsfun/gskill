package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// LegacyBackupName is the safety copy left next to the project after a
// lockfile migration (spec 012 FR-009). Cleaning it up is the user's call.
const LegacyBackupName = "gskill.lock.backup"

// MigrateResult reports a lockfile migration.
type MigrateResult struct {
	Migrated        bool
	AlreadyMigrated bool
	BackupPath      string
	Skills          int
}

// MigrateLockfile converts a legacy gskill.lock into the shared
// skills-lock.json (FR-008..FR-011). Ordering is crash-safe: the backup is
// written first, then the shared lock (atomically), and only then is the
// legacy file removed — no window loses data. When both files exist, the
// shared lock stays canonical and only gains the gskill metadata it lacks.
func (a *App) MigrateLockfile(_ context.Context, root string) (MigrateResult, error) {
	p := openProject(root)
	legacyPath := filepath.Join(root, LockName)

	legacyExists := fileExists(legacyPath)
	sharedExists := fileExists(p.lockPath)
	switch {
	case !legacyExists && !sharedExists:
		return MigrateResult{}, errs.WithHint(
			fmt.Errorf("%w: no %s or %s found", errs.ErrInvalidManifest, LockName, skillslock.FileName),
			"nothing to migrate; run 'gskill add <source>' to install a first skill")
	case !legacyExists:
		return MigrateResult{AlreadyMigrated: true}, nil
	}

	legacyData, err := os.ReadFile(legacyPath) //nolint:gosec // project-root lock path
	if err != nil {
		return MigrateResult{}, fmt.Errorf("read %s: %w", LockName, err)
	}
	legacy, err := lockfile.Unmarshal(legacyData)
	if err != nil {
		return MigrateResult{}, err
	}

	shared, err := mergedSharedLock(p, legacy, sharedExists)
	if err != nil {
		return MigrateResult{}, err
	}
	a.backfillCompatHashes(p, shared)

	backupPath := filepath.Join(root, LegacyBackupName)
	if err := fsutil.WriteFileAtomic(backupPath, legacyData, 0o600); err != nil {
		return MigrateResult{}, fmt.Errorf("write %s: %w", LegacyBackupName, err)
	}
	if err := skillslock.Save(p.lockPath, shared); err != nil {
		return MigrateResult{}, err
	}
	if err := os.Remove(legacyPath); err != nil {
		return MigrateResult{}, fmt.Errorf("retire %s: %w", LockName, err)
	}
	a.log.Info("migrated lockfile", "from", LockName, "to", skillslock.FileName, "backup", LegacyBackupName)
	return MigrateResult{Migrated: true, BackupPath: backupPath, Skills: len(shared.Names())}, nil
}

// mergedSharedLock produces the migration target: a fresh conversion when no
// shared lock exists, otherwise the existing canonical file gaining only the
// gskill metadata it lacks (FR-011 — skills-lock.json always wins).
func mergedSharedLock(p *project, legacy *lockfile.Lockfile, sharedExists bool) (*skillslock.Lock, error) {
	if !sharedExists {
		return skillslock.MigrateFromLegacy(legacy), nil
	}
	shared, err := skillslock.Load(p.lockPath)
	if err != nil {
		// Fail closed: never overwrite a shared file gskill cannot parse.
		return nil, fmt.Errorf("%w: %w", errs.ErrInvalidManifest, err)
	}
	for _, name := range sortedKeys(legacy.Skills) {
		if e, ok := shared.Entry(name); ok && e.Ext != nil {
			continue // canonical entry already carries gskill metadata
		}
		shared.SetEntry(name, skillslock.FromLegacy(legacy.Skills[name]))
	}
	return shared, nil
}

// backfillCompatHashes computes the shared computedHash from store content
// when it is locally available (data-model: "recomputed from store content
// when present; else omitted until next install").
func (a *App) backfillCompatHashes(p *project, shared *skillslock.Lock) {
	for _, name := range shared.Names() {
		e, _ := shared.Entry(name)
		if e.ComputedHash != "" || e.Ext == nil || e.Ext.StoreHash == "" {
			continue
		}
		dir := p.store.Path(e.Ext.StoreHash)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if compat, err := integrity.CompatHash(dir); err == nil {
			e.ComputedHash = compat
			shared.SetEntry(name, e)
		}
	}
}

// maybeMigrate runs the automatic migration for lock-touching commands
// (FR-010/FR-011): any project still holding a legacy gskill.lock is
// converted (or merged into an existing skills-lock.json) before the command
// proceeds, after which the legacy file is never used again.
func (a *App) maybeMigrate(ctx context.Context, root string) error {
	if !fileExists(filepath.Join(root, LockName)) {
		return nil
	}
	_, err := a.MigrateLockfile(ctx, root)
	return err
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// openMigratedProject is the shared prologue of lock-touching commands:
// auto-migrate any legacy lockfile, then open the project and its manifest.
func (a *App) openMigratedProject(ctx context.Context, root string) (*project, *manifest.Manifest, error) {
	if err := a.maybeMigrate(ctx, root); err != nil {
		return nil, nil, err
	}
	p := openProject(root)
	if !p.manifestExists() {
		return nil, nil, errNoManifest()
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return nil, nil, err
	}
	return p, m, nil
}
