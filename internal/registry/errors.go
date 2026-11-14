package registry

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
)

// apiStatusError carries the HTTP status behind a mapped API failure, so
// callers branch on the status rather than on message text.
type apiStatusError struct {
	status int
	err    error
}

func (e *apiStatusError) Error() string { return e.err.Error() }
func (e *apiStatusError) Unwrap() error { return e.err }

// statusError maps a non-200 API response onto the exit codes a user acts on
// (spec 035 research R10).
func statusError(c *client, resp *http.Response, what string) error {
	return &apiStatusError{status: resp.StatusCode, err: statusMessage(c, resp, what)}
}

// statusMessage builds the typed error for statusError. Messages name the
// token's variable, never its value, and never claim a scope does not exist:
// providers answer 404 for private scopes the caller cannot see.
func statusMessage(c *client, resp *http.Response, what string) error {
	authed := c.cred.header != ""
	switch {
	case resp.StatusCode == http.StatusUnauthorized && authed:
		return errs.New(errs.CodeAuth, fmt.Sprintf("%s rejected the token in %s (expired or invalid)", c.provider, c.cred.source))
	case resp.StatusCode == http.StatusUnauthorized:
		return errs.New(errs.CodeAuth, fmt.Sprintf("%s requires a token to list %s; set %s", c.provider, what, c.cred.source))
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-GitHub-SSO") != "":
		return errs.New(errs.CodeAuth, fmt.Sprintf(
			"the token in %s must be authorized for single sign-on in this organization", c.cred.source))
	case rateLimited(resp):
		msg := c.provider + " rate limit reached" + resetSuffix(resp.Header)
		if !authed {
			msg += fmt.Sprintf("; set %s to raise the limit", c.cred.source)
		}
		return errs.New(errs.CodeSourceUnavailable, msg)
	case resp.StatusCode == http.StatusForbidden && authed:
		return errs.New(errs.CodeAuth, fmt.Sprintf("the token in %s is not allowed to list %s", c.cred.source, what))
	case resp.StatusCode == http.StatusForbidden:
		return errs.New(errs.CodeAuth, fmt.Sprintf("%s refused to list %s without a token; set %s", c.provider, what, c.cred.source))
	case resp.StatusCode == http.StatusNotFound:
		return errs.New(errs.CodeSourceUnavailable, what+" not found or not accessible with the current credentials")
	default:
		return errs.New(errs.CodeSourceUnavailable, fmt.Sprintf("%s returned %d for %s", c.provider, resp.StatusCode, what))
	}
}

// rateLimited reports a 429, or a 403 that carries the rate-limit signals
// GitHub uses for exhausted quotas.
func rateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode == http.StatusForbidden &&
		(resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")
}

// resetSuffix says when a rate limit resets, from the epoch reset headers
// (GitHub, GitLab) or Retry-After seconds, or "" when none is given.
func resetSuffix(h http.Header) string {
	for _, key := range []string{"X-RateLimit-Reset", "RateLimit-Reset"} {
		if epoch, err := strconv.ParseInt(h.Get(key), 10, 64); err == nil && epoch > 0 {
			return "; resets at " + time.Unix(epoch, 0).UTC().Format(time.RFC3339)
		}
	}
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs >= 0 {
		return fmt.Sprintf("; retry after %ds", secs)
	}
	return ""
}

func unreachable(host string, err error) error {
	return errs.Wrap(errs.CodeSourceUnavailable, "cannot reach "+host, err)
}

func cancelled(err error) error {
	return errs.Wrap(errs.CodeCancelled, "discovery cancelled", err)
}
