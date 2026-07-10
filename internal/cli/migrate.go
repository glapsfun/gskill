package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// migrateCmd groups one-way format migrations.
type migrateCmd struct {
	Lockfile migrateLockfileCmd `cmd:"" help:"Convert a legacy gskill.lock into skills-lock.json (backs up the original)."`
}

// migrateLockfileCmd converts gskill.lock → skills-lock.json (spec 012 US3).
type migrateLockfileCmd struct{}

// Help returns the detailed help shown by `gskill migrate lockfile --help`.
func (migrateLockfileCmd) Help() string {
	return examplesHelp(
		"gskill migrate lockfile",
	)
}

// Run executes `gskill migrate lockfile`.
func (migrateLockfileCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.MigrateLockfile(ctx, string(root))
	if err != nil {
		return err
	}
	human := fmt.Sprintf("Migrated %d skill(s) from gskill.lock to skills-lock.json (backup: %s)",
		res.Skills, app.LegacyBackupName)
	if res.AlreadyMigrated {
		human = "Already migrated: skills-lock.json is the project lock file"
	}
	human = out.summary(human)
	return out.Result(human, map[string]any{
		"migrated":         res.Migrated,
		"already_migrated": res.AlreadyMigrated,
		"backup":           res.BackupPath,
		"skills":           res.Skills,
	})
}
