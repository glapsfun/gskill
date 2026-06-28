package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
)

// maxResponseBytes caps how much of an API response is read, since the response
// is untrusted input.
const maxResponseBytes = 8 << 20 // 8 MiB

// RepoRef identifies a discovered repository.
type RepoRef struct {
	Name          string
	CloneURL      string
	DefaultBranch string
}

// Client lists a GitHub owner's repositories via the REST API.
type Client struct {
	http    *http.Client
	baseURL string
	token   string
}

// New returns a Client targeting api.github.com, using GITHUB_TOKEN when set.
func New() *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://api.github.com",
		token:   os.Getenv("GITHUB_TOKEN"),
	}
}

// NewWithBase returns a Client targeting a custom base URL (for tests).
func NewWithBase(base string) *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second}, baseURL: base}
}

// ListOwnerRepos returns the public repositories of a GitHub user or org. It
// tries the user endpoint first, then the org endpoint. Responses are treated
// as untrusted input.
func (c *Client) ListOwnerRepos(ctx context.Context, owner string) ([]RepoRef, error) {
	repos, err := c.get(ctx, fmt.Sprintf("%s/users/%s/repos?per_page=100", c.baseURL, owner))
	if err == nil {
		return repos, nil
	}
	repos, orgErr := c.get(ctx, fmt.Sprintf("%s/orgs/%s/repos?per_page=100", c.baseURL, owner))
	if orgErr != nil {
		// Surface the fallback (org) failure: the user-endpoint error is
		// typically just a 404 because the owner is an org, not a user.
		return nil, fmt.Errorf("%w: list repos for %q: %w", errs.ErrSourceUnavailable, owner, orgErr)
	}
	return repos, nil
}

// get fetches and decodes one page of repositories.
func (c *Client) get(ctx context.Context, url string) ([]RepoRef, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrSourceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: github returned %d", errs.ErrSourceUnavailable, resp.StatusCode)
	}

	var raw []struct {
		Name          string `json:"name"`
		CloneURL      string `json:"clone_url"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode github response: %w", err)
	}

	repos := make([]RepoRef, 0, len(raw))
	for _, r := range raw {
		repos = append(repos, RepoRef{Name: r.Name, CloneURL: r.CloneURL, DefaultBranch: r.DefaultBranch})
	}
	return repos, nil
}
