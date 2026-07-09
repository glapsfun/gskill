package cli

import (
	"context"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// initCmd scaffolds a gskill project.
type initCmd struct{}

// Help returns the detailed help shown by `gskill init --help`.
func (initCmd) Help() string {
	return examplesHelp(
		"gskill init",
		"gskill -C path/to/repo init",
	)
}

// Run executes `gskill init`.
func (initCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Init(ctx, string(root))
	if err != nil {
		return err
	}

	created := res.Created
	if len(created) == 0 {
		out.Diag("already initialized")
	}
	human := "Initialized gskill project at " + res.ManifestPath
	human = out.summary(human)
	if len(created) > 0 {
		human += "\nCreated: " + strings.Join(created, ", ")
	}
	return out.Result(human, map[string]any{
		"manifest": res.ManifestPath,
		"created":  created,
	})
}
