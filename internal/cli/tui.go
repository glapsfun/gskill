package cli

import (
	"context"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/tui"
)

// tuiCmd launches the interactive dashboard. Its canonical command name is
// `dashboard`; the original `tui` remains a kong alias.
type tuiCmd struct{}

// Help returns the detailed help shown by `gskill dashboard --help`.
func (tuiCmd) Help() string {
	return examplesHelp("gskill dashboard")
}

// Run executes `gskill dashboard` (alias: `gskill tui`).
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
