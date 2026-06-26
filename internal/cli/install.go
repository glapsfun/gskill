package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// installCmd installs all declared skills.
type installCmd struct {
	Global bool `help:"Install into the user-global location."`
	Copy   bool `help:"Copy instead of symlinking."`
}

// Run executes `gskill install`.
func (c installCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Install(ctx, app.InstallRequest{
		Root:  string(root),
		Scope: scopeFlag(c.Global),
		Mode:  modeFlag(c.Copy),
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
	return out.Result(human, map[string]any{
		"changed": res.Changed,
		"skills":  skills,
	})
}
