package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// updateCmd advances skills to the newest version within their constraints.
type updateCmd struct {
	Skills []string `arg:"" optional:"" help:"Skills to update (default: all declared)."`
}

// Run executes `gskill update`.
func (c updateCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Update(ctx, string(root), c.Skills)
	if err != nil {
		return err
	}
	return renderInstallChanges(out, "Updated", res)
}

// renderInstallChanges renders an install/update/lock result.
func renderInstallChanges(out *Output, verb string, res app.InstallResult) error {
	skills := make([]map[string]any, 0, len(res.Skills))
	changed := 0
	for _, s := range res.Skills {
		if s.Changed {
			changed++
		}
		skills = append(skills, map[string]any{
			"name": s.Name, "content_hash": s.ContentHash, "changed": s.Changed,
		})
	}
	human := fmt.Sprintf("%s %d skill(s); %d changed", verb, len(res.Skills), changed)
	return out.Result(human, map[string]any{"changed": res.Changed, "skills": skills})
}
