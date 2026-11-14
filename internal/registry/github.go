package registry

import (
	"context"
	"net/url"
	"strings"

	"github.com/glapsfun/gskill/internal/source"
)

// listGitHub lists a GitHub user's or organization's repositories. The
// owner's type picks the endpoint: an organization lists everything the
// token's account can see; the token's own account lists its private
// repositories too; any other user lists public ones (GitHub documents
// /users/{name}/repos as public only).
func listGitHub(ctx context.Context, c *client, scope Scope) ([]RepoRef, []string, error) {
	base := strings.TrimRight(c.origin.String(), "/")
	name := url.PathEscape(scope.Path)
	what := scope.String()

	var owner struct {
		Type string `json:"type"`
	}
	if _, err := c.getJSON(ctx, base+"/users/"+name, what, &owner); err != nil {
		return nil, nil, err
	}
	first := base + "/users/" + name + "/repos?per_page=100"
	switch {
	case owner.Type == "Organization":
		first = base + "/orgs/" + name + "/repos?type=all&per_page=100"
	case c.cred.header != "" && isTokenAccount(ctx, c, base, scope.Path):
		first = base + "/user/repos?affiliation=owner&per_page=100"
	}
	return c.collect(ctx, first, func(ctx context.Context, pageURL string) ([]RepoRef, []string, string, error) {
		var page []struct {
			FullName      string `json:"full_name"`
			CloneURL      string `json:"clone_url"`
			SSHURL        string `json:"ssh_url"`
			Private       bool   `json:"private"`
			DefaultBranch string `json:"default_branch"`
		}
		h, err := c.getJSON(ctx, pageURL, what, &page)
		if err != nil {
			return nil, nil, "", err
		}
		repos := make([]RepoRef, 0, len(page))
		var warnings []string
		for _, r := range page {
			ref, warn := newRepoRef(source.ProviderGitHub, r.FullName, r.DefaultBranch, r.Private, r.CloneURL, r.SSHURL)
			if warn != "" {
				warnings = append(warnings, warn)
				continue
			}
			repos = append(repos, ref)
		}
		return repos, warnings, nextLink(h), nil
	})
}

// isTokenAccount reports whether name is the token's own account. Any failure,
// such as an installation token that cannot read /user, means "no".
func isTokenAccount(ctx context.Context, c *client, base, name string) bool {
	var me struct {
		Login string `json:"login"`
	}
	if _, err := c.getJSON(ctx, base+"/user", "the token's account", &me); err != nil {
		return false
	}
	return strings.EqualFold(me.Login, name)
}
