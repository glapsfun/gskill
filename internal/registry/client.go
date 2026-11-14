package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
)

// maxResponseBytes caps how much of an API response is read, since the response
// is untrusted input.
const maxResponseBytes = 8 << 20 // 8 MiB

// maxRepos caps one listing; a scope larger than this is cut short with a
// warning rather than paged through without end.
const maxRepos = 1000

// client talks to one provider API origin. The token is attached by
// authTransport to each request, including each redirect hop, only when the
// request goes to that exact origin (scheme, host, and port), and redirects
// to any other origin are refused: Go's client would otherwise forward
// Authorization to the same host name on another port and to subdomains.
type client struct {
	http   *http.Client
	origin *url.URL
	// cred names where the token came from, for messages; it is never the
	// token itself.
	cred     credential
	provider string
	// accept is the response media type requested; GitHub documents its own.
	accept string
}

func newClient(base string, cred credential, provider string) (*client, error) {
	origin, err := url.Parse(base)
	if err != nil || origin.Host == "" {
		return nil, fmt.Errorf("invalid API base %q", base)
	}
	c := &client{origin: origin, cred: cred, provider: provider, accept: "application/json"}
	c.http = &http.Client{
		Timeout:   15 * time.Second,
		Transport: authTransport{base: http.DefaultTransport, origin: origin, header: cred.header},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// A custom CheckRedirect replaces the client's default 10-hop cap.
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if !sameOrigin(req.URL, origin) {
				return fmt.Errorf("%w to %s", errRedirectRefused, req.URL.Host)
			}
			return nil
		},
	}
	return c, nil
}

// authTransport adds the Authorization header to requests for origin only.
type authTransport struct {
	base   http.RoundTripper
	origin *url.URL
	header string
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.header == "" || !sameOrigin(req.URL, t.origin) {
		return t.base.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", t.header)
	return t.base.RoundTrip(r)
}

// sameOrigin compares scheme, host, and port.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// get fetches pageURL and returns its body and headers. A non-200 status is
// mapped to a typed error naming what was asked for.
func (c *client) get(ctx context.Context, pageURL, what string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", c.accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, c.transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, c.transportError(ctx, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, statusError(c, resp, what)
	}
	return body, resp.Header, nil
}

// getJSON fetches pageURL and decodes its JSON body into v.
func (c *client) getJSON(ctx context.Context, pageURL, what string, v any) (http.Header, error) {
	body, h, err := c.get(ctx, pageURL, what)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", c.provider, err)
	}
	return h, nil
}

// pageFunc fetches one page and returns its repositories, warnings about
// skipped ones, and the raw next-page link ("" on the last page).
type pageFunc func(ctx context.Context, pageURL string) (repos []RepoRef, warnings []string, next string, err error)

// collect follows next-page links from first until the last page or the
// repository cap. A link to another origin ends the listing with a warning,
// and a link already followed ends it quietly.
func (c *client) collect(ctx context.Context, first string, page pageFunc) ([]RepoRef, []string, error) {
	var repos []RepoRef
	var warnings []string
	seen := map[string]bool{}
	for next := first; next != ""; {
		u, err := url.Parse(next)
		if err != nil || !sameOrigin(u, c.origin) {
			warnings = append(warnings, "ignored a next-page link to "+linkHost(u, next))
			break
		}
		if seen[u.String()] {
			break
		}
		seen[u.String()] = true
		batch, warn, link, err := page(ctx, u.String())
		if err != nil {
			return nil, nil, err
		}
		repos, warnings = append(repos, batch...), append(warnings, warn...)
		if len(repos) >= maxRepos {
			if len(repos) > maxRepos || link != "" {
				warnings = append(warnings, fmt.Sprintf("listing stopped at %d repositories", maxRepos))
			}
			return repos[:maxRepos], warnings, nil
		}
		next = resolveLink(u, link)
	}
	return repos, warnings, nil
}

func linkHost(u *url.URL, raw string) string {
	if u != nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// resolveLink resolves a possibly relative next-page link against the page
// it came from.
func resolveLink(page *url.URL, link string) string {
	if link == "" {
		return ""
	}
	ref, err := url.Parse(link)
	if err != nil {
		return link
	}
	return page.ResolveReference(ref).String()
}

// nextLink returns the rel="next" target of an RFC 8288 Link header, as
// GitHub and GitLab paginate.
func nextLink(h http.Header) string {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(strings.ReplaceAll(params, " ", ""), `rel="next"`) {
			continue
		}
		return strings.Trim(strings.TrimSpace(target), "<>")
	}
	return ""
}

// errRedirectRefused marks a redirect to another origin, which the client
// will not follow with the token.
var errRedirectRefused = errors.New("refused a redirect to another origin")

// transportError maps a failed request to a cancellation, a refused
// redirect, or an unreachable API.
func (c *client) transportError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); errors.Is(ctxErr, context.Canceled) {
		return cancelled(ctxErr)
	}
	if errors.Is(err, errRedirectRefused) {
		return errs.Wrap(errs.CodeSourceUnavailable, c.origin.Host+" redirected to another origin", errRedirectRefused)
	}
	return unreachable(c.origin.Host, err)
}
