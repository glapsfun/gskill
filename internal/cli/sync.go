package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// syncCmd reconciles disk to the lockfile.
type syncCmd struct {
	Prune bool `help:"Remove agent skill directories not in the lockfile."`
}

// Run executes `gskill sync`.
func (c syncCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	res, err := a.Sync(ctx, app.SyncRequest{Root: string(root), Prune: c.Prune, Offline: g.Offline})
	if err != nil {
		return err
	}

	for _, p := range res.Pruned {
		out.Diag("pruned: %s", p)
	}
	human := fmt.Sprintf("Reconciled %d skill(s); pruned %d", len(res.Reconciled), len(res.Pruned))
	return out.Result(human, map[string]any{
		"reconciled": len(res.Reconciled),
		"pruned":     res.Pruned,
	})
}
