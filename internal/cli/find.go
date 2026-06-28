package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// findCmd searches for skills within a source, a GitHub owner, or the configured
// repositories, always including locally installed skills.
type findCmd struct {
	Query  string `arg:"" help:"Keyword to search for (matches name and description)."`
	Source string `help:"Search within one source."`
	Owner  string `help:"Search a GitHub user/org's repositories."`
}

// Run executes `gskill find`.
func (c findCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	hits, warnings, err := a.Find(ctx, c.Query, app.FindScope{
		Source: c.Source,
		Owner:  c.Owner,
		Root:   string(root),
	})
	if err != nil {
		return err
	}
	for _, w := range warnings {
		out.Diag("warning: %s", w)
	}

	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		marker := ""
		if h.Installed {
			marker = " (installed)"
		}
		lines = append(lines, fmt.Sprintf("%-28s %-40s %s%s", h.ID, h.Source, pathOrRoot(h.RepoPath), marker))
	}
	human := "no matching skills found"
	if len(lines) > 0 {
		human = strings.Join(lines, "\n")
	}
	return out.Result(human, hits)
}
