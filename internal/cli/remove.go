package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// removeCmd uninstalls skills and cleans up the manifest, lock, and agent dirs.
type removeCmd struct {
	Skills []string `arg:"" help:"Skills to remove."`
}

// Run executes `gskill remove`.
func (c removeCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Remove(ctx, string(root), c.Skills)
	if err != nil {
		return err
	}

	for _, name := range res.NotPresent {
		out.Diag("not installed: %s", name)
	}
	human := fmt.Sprintf("Removed %d skill(s); GC'd %d store entr(ies)", len(res.Removed), res.StoreGCed)
	return out.Result(human, map[string]any{
		"removed":     res.Removed,
		"store_gced":  res.StoreGCed,
		"not_present": res.NotPresent,
	})
}
