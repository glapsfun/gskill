package cli

import (
	"context"

	"github.com/glapsfun/gskill/internal/app"
)

// lockCmd recomputes the lockfile from the manifest without bumping pins.
type lockCmd struct{}

// Help returns the detailed help shown by `gskill project lock --help`.
func (lockCmd) Help() string {
	return examplesHelp(
		"gskill project lock",
	)
}

// Run executes `gskill project lock` (alias: `gskill lock`).
func (lockCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Lock(ctx, string(root))
	if err != nil {
		return err
	}
	return renderInstallChanges(out, "Locked", res)
}
