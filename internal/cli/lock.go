package cli

import (
	"context"

	"github.com/glapsfun/gskill/internal/app"
)

// lockCmd recomputes the lockfile from the manifest without bumping pins.
type lockCmd struct{}

// Run executes `gskill lock`.
func (lockCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Lock(ctx, string(root))
	if err != nil {
		return err
	}
	return renderInstallChanges(out, "Locked", res)
}
