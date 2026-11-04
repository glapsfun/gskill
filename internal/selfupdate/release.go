package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/errs"
)

// DefaultAPIBase is the GitHub API base for the gskill repository.
const DefaultAPIBase = "https://api.github.com/repos/glapsfun/gskill"

// Size caps, since every response is untrusted input. Release archives are
// about 10 MB; the caps leave room without letting a bad response fill a disk.
const (
	maxMetadataBytes = 8 << 20
	maxManifestBytes = 1 << 20
	maxBundleBytes   = 1 << 20
	maxArchiveBytes  = 128 << 20
)

const releasesURL = "https://github.com/glapsfun/gskill/releases"

// GitHubSource reads gskill releases from the GitHub REST API.
type GitHubSource struct {
	api     *http.Client
	assets  *http.Client
	apiBase string
	token   string
}

// NewGitHubSource returns a source for the repository at apiBase. A nil client
// builds the production clients: metadata requests time out after 15 seconds,
// while asset downloads only bound the wait for response headers, so a slow
// link can still finish a large archive. token, when set, is sent to the API
// only, never to the asset host.
func NewGitHubSource(client *http.Client, apiBase, token string) *GitHubSource {
	s := &GitHubSource{api: client, assets: client, apiBase: strings.TrimRight(apiBase, "/"), token: token}
	if client == nil {
		assetTransport := &http.Transport{Proxy: http.ProxyFromEnvironment}
		if tr, ok := http.DefaultTransport.(*http.Transport); ok {
			assetTransport = tr.Clone()
		}
		assetTransport.ResponseHeaderTimeout = 30 * time.Second
		s.api = &http.Client{Timeout: 15 * time.Second}
		s.assets = &http.Client{Transport: assetTransport}
	}
	return s
}

// Latest returns the newest stable release. GitHub's "latest" never returns a
// draft or a prerelease.
func (s *GitHubSource) Latest(ctx context.Context) (Release, error) {
	return s.release(ctx, s.apiBase+"/releases/latest", "")
}

// ByTag returns the release published under tag.
func (s *GitHubSource) ByTag(ctx context.Context, tag string) (Release, error) {
	return s.release(ctx, s.apiBase+"/releases/tags/"+url.PathEscape(tag), tag)
}

func (s *GitHubSource) release(ctx context.Context, u, tag string) (Release, error) {
	resp, err := s.get(ctx, s.api, u)
	if err != nil {
		return Release{}, errs.Wrap(errs.CodeSourceUnavailable, "read gskill releases", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case rateLimited(resp):
		return Release{}, errs.WithHint(
			errs.New(errs.CodeSourceUnavailable, "GitHub API rate limit reached while reading gskill releases"),
			"wait and retry, or set GITHUB_TOKEN")
	case resp.StatusCode == http.StatusNotFound && tag != "":
		return Release{}, errs.WithHint(errs.New(errs.CodeUsage, tag+" is not a published gskill release"), "see "+releasesURL)
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, errs.WithHint(errs.New(errs.CodeSourceUnavailable, "no stable gskill release is published"), "see "+releasesURL)
	case resp.StatusCode != http.StatusOK:
		return Release{}, errs.New(errs.CodeSourceUnavailable,
			fmt.Sprintf("read gskill releases: GitHub returned %d", resp.StatusCode))
	}

	var raw struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&raw); err != nil {
		return Release{}, errs.Wrap(errs.CodeSourceUnavailable, "decode gskill release metadata", err)
	}

	v, err := semver.StrictNewVersion(strings.TrimPrefix(raw.TagName, "v"))
	if err != nil {
		return Release{}, errs.Wrap(errs.CodeSourceUnavailable,
			fmt.Sprintf("release tag %q is not a semantic version", raw.TagName), err)
	}
	if raw.Draft {
		return Release{}, errs.New(errs.CodeSourceUnavailable, "release "+raw.TagName+" is a draft")
	}
	rel := Release{Version: v, Tag: raw.TagName, Prerelease: raw.Prerelease}
	for _, a := range raw.Assets {
		if !strings.HasPrefix(a.URL, "https://") {
			return Release{}, errs.New(errs.CodeIntegrity,
				fmt.Sprintf("release asset %s does not use https", a.Name))
		}
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return rel, nil
}

// Open starts downloading a. The body fails once it passes a.Size, and a.Size
// itself must be within the cap for that kind of asset.
func (s *GitHubSource) Open(ctx context.Context, a Asset) (io.ReadCloser, error) {
	limit := assetCap(a.Name)
	if a.Size > limit {
		return nil, errs.New(errs.CodeIntegrity,
			fmt.Sprintf("%s is %d bytes, over the %d-byte limit", a.Name, a.Size, limit))
	}
	resp, err := s.get(ctx, s.assets, a.URL)
	if err != nil {
		return nil, errs.Wrap(errs.CodeSourceUnavailable, "download "+a.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errs.New(errs.CodeSourceUnavailable,
			fmt.Sprintf("download %s: GitHub returned %d", a.Name, resp.StatusCode))
	}
	return &sizedBody{body: resp.Body, left: a.Size, name: a.Name}, nil
}

func (s *GitHubSource) get(ctx context.Context, client *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if strings.HasPrefix(u, s.apiBase+"/") {
		req.Header.Set("Accept", "application/vnd.github+json")
		if s.token != "" {
			req.Header.Set("Authorization", "Bearer "+s.token)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", u, err)
	}
	return resp, nil
}

// rateLimited reports GitHub's rate-limit answer: 429, or 403 with no
// requests left.
func rateLimited(resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0")
}

func assetCap(name string) int64 {
	switch {
	case name == "checksums.txt":
		return maxManifestBytes
	case strings.HasSuffix(name, ".sigstore.json"):
		return maxBundleBytes
	case strings.HasSuffix(name, ".tar.gz"):
		return maxArchiveBytes
	default:
		return maxManifestBytes
	}
}

// sizedBody fails a download that sends more than the release said it would.
type sizedBody struct {
	body io.ReadCloser
	left int64
	name string
}

func (b *sizedBody) Read(p []byte) (int, error) {
	if b.left < 0 {
		return 0, errs.New(errs.CodeIntegrity, b.name+" is larger than the release says")
	}
	if int64(len(p)) > b.left+1 {
		p = p[:b.left+1]
	}
	n, err := b.body.Read(p)
	b.left -= int64(n)
	if b.left < 0 {
		return n, errs.New(errs.CodeIntegrity, b.name+" is larger than the release says")
	}
	return n, err
}

func (b *sizedBody) Close() error { return b.body.Close() }
