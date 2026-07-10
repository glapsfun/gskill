package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// diffCmd reports manifest/lock/disk differences.
type diffCmd struct{}

// Help returns the detailed help shown by `gskill project diff --help`.
func (diffCmd) Help() string {
	return examplesHelp(
		"gskill project diff",
		"gskill project diff --json",
	)
}

// Run executes `gskill project diff` (alias: `gskill diff`).
func (diffCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	entries, err := a.Diff(ctx, string(root))
	if err != nil {
		return err
	}

	rows := make([]map[string]any, 0, len(entries))
	var b strings.Builder
	for _, e := range entries {
		_, _ = fmt.Fprintf(&b, "%-24s %s\n", e.Name, e.Status)
		rows = append(rows, map[string]any{
			"name": e.Name, "status": e.Status,
		})
	}
	human := strings.TrimRight(b.String(), "\n")
	if human == "" {
		human = "No skills declared."
	}
	if out.Interactive() {
		human = renderDiffStyled(entries)
	}
	return out.Result(human, map[string]any{"entries": rows})
}
