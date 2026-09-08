package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// repairCmd re-materializes broken installs and cleans up staging.
type repairCmd struct{}

// Help returns the detailed help shown by `gskill project repair --help`.
func (repairCmd) Help() string {
	return examplesHelp(
		"gskill project repair",
	)
}

// Run executes `gskill project repair`.
func (repairCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	ctx, done := out.withFetchProgress(ctx)
	defer done()
	res, err := a.Repair(ctx, string(root))
	done()
	if err != nil {
		return err
	}
	human := fmt.Sprintf("Repaired %d skill(s)", len(res.Repaired))
	human = out.summary(human)
	return out.Result(human, map[string]any{
		"repaired": res.Repaired,
	})
}
