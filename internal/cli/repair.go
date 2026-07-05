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

// Run executes `gskill project repair` (alias: `gskill repair`).
func (repairCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Repair(ctx, string(root))
	if err != nil {
		return err
	}
	human := fmt.Sprintf("Repaired %d skill(s); cleaned %d staging dir(s)", len(res.Repaired), res.StagingCleaned)
	return out.Result(human, map[string]any{
		"repaired":        res.Repaired,
		"staging_cleaned": res.StagingCleaned,
	})
}
