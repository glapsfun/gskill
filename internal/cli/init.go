package cli

import (
	"context"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// initCmd prepares local gskill runtime state.
type initCmd struct {
	Lock bool `help:"Also create an empty skills-lock.json."`
}

// Help returns the detailed help shown by `gskill init --help`.
func (initCmd) Help() string {
	return describedHelp(
		"add and install already perform this initialization automatically when needed "+
			"(.gskill/, .agents/, .gitignore) — running init directly is only necessary to "+
			"prepare a project ahead of time or to pass --lock.",
		"gskill init",
		"gskill init --lock",
		"gskill -C path/to/repo init",
	)
}

// Run executes `gskill init`.
func (c initCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Init(ctx, string(root), c.Lock)
	if err != nil {
		return err
	}

	created := res.Created
	if len(created) == 0 {
		out.Info("already initialized")
	}
	human := out.summary("Initialized gskill local state")
	if len(created) > 0 {
		human += "\nCreated: " + strings.Join(created, ", ")
	}
	return out.Result(human, map[string]any{
		"lock":    res.LockPath,
		"created": created,
	})
}
