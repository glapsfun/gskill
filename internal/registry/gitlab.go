package registry

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/glapsfun/gskill/internal/source"
)

// listGitLab lists the projects of a GitLab group, including its subgroups,
// or of a user when no group has a single-segment path.
func listGitLab(ctx context.Context, c *client, scope Scope) ([]RepoRef, []string, error) {
	base := strings.TrimRight(c.origin.String(), "/")
	what := scope.String()
	page := gitlabPage(c, what)

	repos, warnings, err := c.collect(ctx,
		base+"/groups/"+url.PathEscape(scope.Path)+"/projects?include_subgroups=true&per_page=100", page)
	if err == nil || strings.Contains(scope.Path, "/") || !notFound(err) {
		return repos, warnings, err
	}
	return c.collect(ctx, base+"/users/"+url.PathEscape(scope.Path)+"/projects?per_page=100", page)
}

func gitlabPage(c *client, what string) pageFunc {
	return func(ctx context.Context, pageURL string) ([]RepoRef, []string, string, error) {
		var page []struct {
			PathWithNamespace string `json:"path_with_namespace"`
			HTTPURL           string `json:"http_url_to_repo"`
			SSHURL            string `json:"ssh_url_to_repo"`
			Visibility        string `json:"visibility"`
			DefaultBranch     string `json:"default_branch"`
		}
		h, err := c.getJSON(ctx, pageURL, what, &page)
		if err != nil {
			return nil, nil, "", err
		}
		repos := make([]RepoRef, 0, len(page))
		var warnings []string
		for _, p := range page {
			ref, warn := newRepoRef(source.ProviderGitLab, p.PathWithNamespace, p.DefaultBranch, p.Visibility != "public", p.HTTPURL, p.SSHURL)
			if warn != "" {
				warnings = append(warnings, warn)
				continue
			}
			repos = append(repos, ref)
		}
		return repos, warnings, nextLink(h), nil
	}
}

// notFound reports a 404 from the API.
func notFound(err error) bool {
	var e *apiStatusError
	return errors.As(err, &e) && e.status == http.StatusNotFound
}
