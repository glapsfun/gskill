package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// removeCmd uninstalls skills and cleans up the manifest, lock, and agent dirs.
type removeCmd struct {
	Skills []string `arg:"" help:"Skills to remove."`
}

// Help returns the detailed help shown by `gskill remove --help`.
func (removeCmd) Help() string {
	return examplesHelp(
		"gskill remove my-skill",
		"gskill remove my-skill other-skill --yes",
	)
}

// Run executes `gskill remove`.
func (c removeCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	prompt := fmt.Sprintf("Remove %s?", strings.Join(c.Skills, ", "))
	if !out.Confirm(prompt, g.Yes) {
		return errs.New(errs.CodeGeneric, "aborted")
	}
	res, err := a.Remove(ctx, string(root), c.Skills)
	if err != nil {
		return err
	}

	for _, name := range res.NotPresent {
		out.Diag("not installed: %s", name)
	}
	human := fmt.Sprintf("Removed %d skill(s); GC'd %d store entr(ies)", len(res.Removed), res.StoreGCed)
	if out.Interactive() {
		human = styledSummary(human)
	}
	return out.Result(human, map[string]any{
		"removed":     res.Removed,
		"store_gced":  res.StoreGCed,
		"not_present": res.NotPresent,
	})
}
