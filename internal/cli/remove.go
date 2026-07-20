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
	Force  bool     `help:"Skip confirmation and remove without a terminal."`
}

// removeRequiresForce reports whether a remove invocation must be aborted
// for lack of confirmation: a non-interactive session (no terminal available
// to prompt) with no opt-in (neither the global --yes nor the local --force).
// Interactive sessions always defer to the existing Confirm prompt instead.
func removeRequiresForce(interactive, optedIn bool) bool {
	return !interactive && !optedIn
}

// Help returns the detailed help shown by `gskill remove --help`.
func (removeCmd) Help() string {
	return examplesHelp(
		"gskill remove my-skill",
		"gskill remove my-skill other-skill --yes",
		"gskill remove my-skill other-skill --force",
	)
}

// Run executes `gskill remove`.
func (c removeCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	optedIn := g.Yes || c.Force
	if removeRequiresForce(out.Interactive(), optedIn) {
		return errs.WithHint(errs.New(errs.CodeGeneric, "remove requires confirmation"), "pass --force to remove without a prompt")
	}
	prompt := fmt.Sprintf("Remove %s?", strings.Join(c.Skills, ", "))
	if !out.Confirm(prompt, optedIn) {
		return errs.New(errs.CodeGeneric, "aborted")
	}
	res, err := a.Remove(ctx, string(root), c.Skills)
	if err != nil {
		return err
	}

	for _, name := range res.NotPresent {
		out.Info("not installed: %s", name)
	}
	human := fmt.Sprintf("Removed %d skill(s); GC'd %d store entr(ies)", len(res.Removed), res.StoreGCed)
	human = out.summary(human)
	return out.Result(human, map[string]any{
		"removed":     res.Removed,
		"store_gced":  res.StoreGCed,
		"not_present": res.NotPresent,
	})
}
