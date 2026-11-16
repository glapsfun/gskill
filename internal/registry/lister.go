package registry

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
)

// Lister lists the repositories in a discovery scope that the configured
// credentials can see.
type Lister interface {
	ListRepos(ctx context.Context, scope Scope) (Listing, error)
}

// Listing is one scope's repositories plus warnings about what was skipped
// or cut short.
type Listing struct {
	Repos         []RepoRef
	Warnings      []string
	Authenticated bool
}

// RepoRef is a discovered repository. The HTTPS address is built by GSKILL
// from the full path on the provider's Git host. The SSH address is the one
// the provider reported when it names the same repository on one of the
// provider's own SSH hosts (Bitbucket reports ssh.bitbucket.org), else it is
// built the same way.
type RepoRef struct {
	Provider      source.Provider
	FullPath      string
	HTTPSURL      string
	SSHURL        string
	DefaultBranch string
	Private       bool
}

// CloneURL returns the address for transport t.
func (r RepoRef) CloneURL(t Transport) string {
	if t == TransportSSH {
		return r.SSHURL
	}
	return r.HTTPSURL
}

// defaultBases are the cloud API origins.
var defaultBases = map[source.Provider]string{
	source.ProviderGitHub:    "https://api.github.com",
	source.ProviderGitLab:    "https://gitlab.com/api/v4",
	source.ProviderBitbucket: "https://api.bitbucket.org/2.0",
}

// providerNames are the names messages use.
var providerNames = map[source.Provider]string{
	source.ProviderGitHub:    "GitHub",
	source.ProviderGitLab:    "GitLab",
	source.ProviderBitbucket: "Bitbucket",
}

type apiLister struct {
	bases map[source.Provider]string
	env   func(string) string
}

// NewLister returns a Lister for the three cloud providers that reads tokens
// through env (os.Getenv in production).
func NewLister(env func(string) string) Lister {
	return apiLister{bases: defaultBases, env: env}
}

// NewListerWithBases returns a Lister whose API origins are bases, for tests.
func NewListerWithBases(bases map[source.Provider]string, env func(string) string) Lister {
	return apiLister{bases: bases, env: env}
}

// ListRepos lists scope's repositories, reading every page up to maxRepos.
func (l apiLister) ListRepos(ctx context.Context, scope Scope) (Listing, error) {
	base, ok := l.bases[scope.Provider]
	if !ok {
		return Listing{}, errs.New(errs.CodeUsage, fmt.Sprintf("discovery does not support %s", scope.Provider))
	}
	cred := credentialFor(scope.Provider, l.env)
	c, err := newClient(base, cred, providerNames[scope.Provider])
	if err != nil {
		return Listing{}, err
	}
	if scope.Provider == source.ProviderGitHub {
		c.accept = "application/vnd.github+json"
	}
	var repos []RepoRef
	var warnings []string
	switch scope.Provider {
	case source.ProviderGitLab:
		repos, warnings, err = listGitLab(ctx, c, scope)
	case source.ProviderBitbucket:
		repos, warnings, err = listBitbucket(ctx, c, scope)
	case source.ProviderGitHub:
		repos, warnings, err = listGitHub(ctx, c, scope)
	case source.ProviderGeneric:
		return Listing{}, errs.New(errs.CodeUsage, "discovery does not support generic Git hosts")
	}
	if err != nil {
		return Listing{}, err
	}
	if cred.header == "" {
		warnings = append(warnings, fmt.Sprintf("listing public repositories only; set %s to include private ones", cred.source))
	}
	if len(repos) == 0 {
		warnings = append(warnings, fmt.Sprintf("no repositories are visible in %s with the current credentials", scope))
	}
	return Listing{Repos: repos, Warnings: warnings, Authenticated: cred.header != ""}, nil
}

// credential is an Authorization header value and the variable it came from
// (or would come from).
type credential struct {
	header string
	source string
}

// credentialFor reads the provider's token variables in their documented
// order (spec 035 research R9).
func credentialFor(p source.Provider, env func(string) string) credential {
	switch p {
	case source.ProviderGitLab:
		return bearer(env, "GITLAB_TOKEN")
	case source.ProviderBitbucket:
		token := env("BITBUCKET_TOKEN")
		if token == "" {
			return credential{source: "BITBUCKET_TOKEN"}
		}
		if email := env("BITBUCKET_EMAIL"); email != "" {
			basic := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
			return credential{header: "Basic " + basic, source: "BITBUCKET_EMAIL and BITBUCKET_TOKEN"}
		}
		return credential{header: "Bearer " + token, source: "BITBUCKET_TOKEN"}
	case source.ProviderGitHub, source.ProviderGeneric:
		return bearer(env, "GITHUB_TOKEN", "GH_TOKEN")
	}
	return credential{}
}

func bearer(env func(string) string, vars ...string) credential {
	for _, v := range vars {
		if token := env(v); token != "" {
			return credential{header: "Bearer " + token, source: v}
		}
	}
	return credential{source: vars[0]}
}

// newRepoRef builds a discovered repository from its full path, or returns a
// warning when the path is unusable or the API reported a clone address on
// another host.
func newRepoRef(p source.Provider, fullPath, defaultBranch string, private bool, reported ...string) (RepoRef, string) {
	host := p.GitHost()
	for _, seg := range strings.Split(fullPath, "/") {
		if !segmentRE.MatchString(seg) || strings.Trim(seg, ".") == "" {
			return RepoRef{}, fmt.Sprintf("skipped a repository with an unusable path %q", fullPath)
		}
	}
	if strings.Count(fullPath, "/") < 1 {
		return RepoRef{}, fmt.Sprintf("skipped a repository with an unusable path %q", fullPath)
	}
	sshURL := "git@" + host + ":" + fullPath + ".git"
	for _, addr := range reported {
		if addr == "" {
			continue
		}
		if h := cloneHost(addr); source.ProviderOf(h) != p {
			return RepoRef{}, fmt.Sprintf("skipped %s: its clone address names another host (%s)", fullPath, h)
		}
		if isSSH(addr) && (strings.HasSuffix(addr, ":"+fullPath+".git") || strings.HasSuffix(addr, "/"+fullPath+".git")) {
			sshURL = addr
		}
	}
	return RepoRef{
		Provider:      p,
		FullPath:      fullPath,
		HTTPSURL:      "https://" + host + "/" + fullPath + ".git",
		SSHURL:        sshURL,
		DefaultBranch: defaultBranch,
		Private:       private,
	}, ""
}

// isSSH reports an ssh:// or scp-style address.
func isSSH(addr string) bool {
	return strings.HasPrefix(addr, "ssh://") || (!strings.Contains(addr, "://") && strings.Contains(addr, "@"))
}

// cloneHost returns the lowercased host name of an HTTPS, ssh://, or
// scp-style clone address.
func cloneHost(addr string) string {
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		return strings.ToLower(u.Hostname())
	}
	if _, rest, ok := strings.Cut(addr, "@"); ok {
		host, _, _ := strings.Cut(rest, ":")
		return strings.ToLower(host)
	}
	return ""
}
