package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// installCmd installs all declared skills.
type installCmd struct {
	Global         bool `xor:"scope" help:"Install into the user-global location."`
	Project        bool `xor:"scope" help:"Install into the project (default)."`
	Copy           bool `help:"Copy instead of symlinking."`
	FrozenLockfile bool `name:"frozen-lockfile" help:"Restore exactly from the lockfile; never modify it."`
	UpdateLockfile bool `name:"update-lockfile" help:"Allow the lockfile to be rewritten."`
}

// Help returns the detailed help shown by `gskill install --help`.
func (installCmd) Help() string {
	return examplesHelp(
		"gskill install",
		"gskill install --frozen-lockfile",
		"gskill install --global --copy",
	)
}

// Run executes `gskill install`. The --offline and --no-cache flags are global.
func (c installCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	res, err := a.Install(ctx, app.InstallRequest{
		Root:           string(root),
		Scope:          scopeFlag(c.Global),
		Mode:           modeFromFlags(c.Copy, false, false),
		Frozen:         c.FrozenLockfile,
		Offline:        g.Offline,
		NoCache:        g.NoCache,
		UpdateLockfile: c.UpdateLockfile,
	})
	if err != nil {
		return err
	}

	skills := make([]map[string]any, 0, len(res.Skills))
	changedCount := 0
	for _, s := range res.Skills {
		if s.Changed {
			changedCount++
		}
		skills = append(skills, map[string]any{
			"name":         s.Name,
			"content_hash": s.ContentHash,
			"changed":      s.Changed,
		})
	}

	human := fmt.Sprintf("Installed %d skill(s); %d changed", len(res.Skills), changedCount)
	if !res.Changed {
		human = fmt.Sprintf("Up to date (%d skill(s), no changes)", len(res.Skills))
	}
	if out.Interactive() {
		human = styledSummary(human)
	}
	return out.Result(human, map[string]any{
		"changed": res.Changed,
		"skills":  skills,
	})
}
