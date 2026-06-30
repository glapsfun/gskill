package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// syncCmd reconciles disk to the manifest's desired state.
type syncCmd struct {
	Prune bool `help:"Remove agent targets and active entries the manifest no longer declares."`
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
	for _, o := range res.Orphans {
		out.Diag("orphan (run with --prune to remove): %s", o)
	}

	changed := 0
	for _, c := range res.Reconciled {
		if c.Changed {
			changed++
		}
	}
	human := "Already up to date"
	if !res.UpToDate {
		human = fmt.Sprintf("Reconciled %d skill(s) (%d changed); pruned %d", len(res.Reconciled), changed, len(res.Pruned))
	}
	return out.Result(human, map[string]any{
		"reconciled": res.Reconciled,
		"pruned":     res.Pruned,
		"orphans":    res.Orphans,
		"up_to_date": res.UpToDate,
	})
}
