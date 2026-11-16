package registry

import (
	"context"
	"net/url"
	"strings"

	"github.com/glapsfun/gskill/internal/source"
)

// listBitbucket lists a Bitbucket Cloud workspace's repositories, following
// the JSON next link Bitbucket paginates with.
func listBitbucket(ctx context.Context, c *client, scope Scope) ([]RepoRef, []string, error) {
	base := strings.TrimRight(c.origin.String(), "/")
	what := scope.String()
	first := base + "/repositories/" + url.PathEscape(scope.Path) + "?pagelen=100"
	return c.collect(ctx, first, func(ctx context.Context, pageURL string) ([]RepoRef, []string, string, error) {
		var page struct {
			Values []struct {
				FullName   string `json:"full_name"`
				IsPrivate  bool   `json:"is_private"`
				MainBranch struct {
					Name string `json:"name"`
				} `json:"mainbranch"`
				Links struct {
					Clone []struct {
						Href string `json:"href"`
					} `json:"clone"`
				} `json:"links"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if _, err := c.getJSON(ctx, pageURL, what, &page); err != nil {
			return nil, nil, "", err
		}
		repos := make([]RepoRef, 0, len(page.Values))
		var warnings []string
		for _, r := range page.Values {
			reported := make([]string, 0, len(r.Links.Clone))
			for _, l := range r.Links.Clone {
				reported = append(reported, l.Href)
			}
			ref, warn := newRepoRef(source.ProviderBitbucket, r.FullName, r.MainBranch.Name, r.IsPrivate, reported...)
			if warn != "" {
				warnings = append(warnings, warn)
				continue
			}
			repos = append(repos, ref)
		}
		return repos, warnings, page.Next, nil
	})
}
