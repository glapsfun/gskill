package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// searchCmd searches for skills within a source, a provider scope (a GitHub
// owner, a GitLab group, or a Bitbucket workspace), or the configured
// repositories, always including locally installed skills. Its canonical
// name is `search`; the original `find` remains a kong alias.
type searchCmd struct {
	Query     string `arg:"" help:"Keyword to search for (matches name and description)."`
	Source    string `help:"Search within one source."`
	Owner     string `help:"Search a GitHub user or organization (acme or github.com/acme), a GitLab group (gitlab.com/acme/platform), or a Bitbucket workspace (bitbucket.org/acme)."`
	Transport string `enum:"https,ssh" default:"https" help:"Clone address used for repositories found with --owner: https or ssh."`
}

// Help returns the detailed help shown by `gskill search --help`.
func (searchCmd) Help() string {
	return examplesHelp(
		"gskill search kubernetes",
		"gskill search deploy --owner acme",
		"gskill search deploy --owner gitlab.com/acme/platform",
		"gskill search deploy --owner bitbucket.org/acme --transport ssh",
		"gskill search helper --source github.com/owner/repo",
	)
}

// Run executes `gskill search` (alias: `gskill find`).
func (c searchCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	if c.Transport != "https" && c.Owner == "" {
		return errs.New(errs.CodeUsage, "--transport applies only to repositories found with --owner")
	}
	hits, warnings, err := a.Find(ctx, c.Query, app.FindScope{
		Source:    c.Source,
		Owner:     c.Owner,
		Root:      string(root),
		Transport: c.Transport,
	})
	if err != nil {
		return err
	}
	for _, w := range warnings {
		out.Warn("warning: %s", w)
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
	if out.Interactive() {
		human = renderFindStyled(hits)
	}
	return out.Result(human, hits)
}
