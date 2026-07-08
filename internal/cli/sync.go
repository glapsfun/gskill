package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// syncCmd reconciles disk to the manifest's desired state.
type syncCmd struct {
	Prune bool `help:"Remove agent targets and active entries the manifest no longer declares."`
}

// Help returns the detailed help shown by `gskill project sync --help`.
func (syncCmd) Help() string {
	return examplesHelp(
		"gskill project sync",
		"gskill project sync --prune",
	)
}

// Run executes `gskill project sync` (alias: `gskill sync`).
func (c syncCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	if c.Prune && !out.Confirm("Prune skills and targets the manifest no longer declares?", g.Yes) {
		return errs.New(errs.CodeGeneric, "aborted")
	}
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
	if out.Interactive() {
		human = styledSummary(human)
	}
	return out.Result(human, map[string]any{
		"reconciled": res.Reconciled,
		"pruned":     res.Pruned,
		"orphans":    res.Orphans,
		"up_to_date": res.UpToDate,
	})
}
