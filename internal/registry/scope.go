package registry

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
)

// Transport selects which clone address of a discovered repository is
// scanned and reported.
type Transport string

// Transports for discovered repositories. HTTPS is the default.
const (
	TransportHTTPS Transport = "https"
	TransportSSH   Transport = "ssh"
)

// Scope is where discovery looks: a GitHub user or organization, a GitLab
// group or user (Path may hold subgroups), or a Bitbucket workspace.
type Scope struct {
	Provider  source.Provider
	Path      string
	Transport Transport
}

// String returns the scope as the user would write it host-qualified.
func (s Scope) String() string {
	return s.Provider.GitHost() + "/" + s.Path
}

// segmentRE matches one owner, group, or workspace path segment.
var segmentRE = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9._-]*$`)

// ParseScope parses a --owner value. A bare name is a GitHub owner, as it
// always was; a host-qualified value (github.com/acme, gitlab.com/acme/platform,
// bitbucket.org/acme, optionally with https://) selects the provider.
func ParseScope(raw string) (Scope, error) {
	trimmed := strings.TrimSpace(raw)
	for _, scheme := range []string{"https://", "http://"} {
		trimmed = strings.TrimPrefix(trimmed, scheme)
	}
	segs := strings.Split(strings.Trim(trimmed, "/"), "/")
	scope := Scope{Provider: source.ProviderGitHub, Transport: TransportHTTPS}
	if len(segs) > 1 || strings.Contains(segs[0], ".") {
		scope.Provider = source.ProviderOf(segs[0])
		if scope.Provider == source.ProviderGeneric {
			return Scope{}, errs.New(errs.CodeUsage, fmt.Sprintf(
				"discovery supports github.com, gitlab.com, and bitbucket.org; %q is not one of them", segs[0]))
		}
		segs = segs[1:]
	}
	if len(segs) == 0 || (len(segs) > 1 && scope.Provider != source.ProviderGitLab) {
		return Scope{}, errs.New(errs.CodeUsage, fmt.Sprintf(
			"--owner %q: name one %s", raw, scopeNoun(scope.Provider)))
	}
	for _, seg := range segs {
		if !segmentRE.MatchString(seg) || strings.Trim(seg, ".") == "" {
			return Scope{}, errs.New(errs.CodeUsage, fmt.Sprintf("--owner %q: %q is not a valid name", raw, seg))
		}
	}
	scope.Path = strings.Join(segs, "/")
	return scope, nil
}

// ParseTransport parses a --transport value; "" means HTTPS.
func ParseTransport(raw string) (Transport, error) {
	switch Transport(strings.ToLower(strings.TrimSpace(raw))) {
	case "", TransportHTTPS:
		return TransportHTTPS, nil
	case TransportSSH:
		return TransportSSH, nil
	default:
		return "", errs.New(errs.CodeUsage, fmt.Sprintf("--transport %q: use https or ssh", raw))
	}
}

func scopeNoun(p source.Provider) string {
	switch p {
	case source.ProviderGitLab:
		return "group or user"
	case source.ProviderBitbucket:
		return "workspace"
	case source.ProviderGitHub, source.ProviderGeneric:
		return "user or organization"
	}
	return "owner"
}
