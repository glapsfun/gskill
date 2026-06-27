package cli

import (
	"context"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/tui"
)

// tuiCmd launches the interactive dashboard.
type tuiCmd struct{}

// Run executes `gskill tui`.
func (tuiCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	skills, err := a.List(ctx, string(root))
	if err != nil {
		return err
	}

	rows := make([]tui.SkillRow, 0, len(skills))
	for _, s := range skills {
		markdown, mdErr := a.SkillMarkdown(ctx, string(root), s.Name)
		if mdErr != nil {
			markdown = "# " + s.Name + "\n\n_" + s.Status + "_\n"
		}
		rows = append(rows, tui.SkillRow{Name: s.Name, Status: s.Status, Markdown: markdown})
	}
	return tui.Run(rows, out.Interactive())
}
