package source

import (
	"net"
	"strings"
)

// Provider names the hosting service behind a Git source. It decides where
// the repository ends in a source path and which discovery API applies.
type Provider string

// Providers. ProviderGeneric covers every other host, including SSH config
// aliases, which GSKILL cannot map to a service.
const (
	ProviderGitHub    Provider = "github"
	ProviderGitLab    Provider = "gitlab"
	ProviderBitbucket Provider = "bitbucket"
	ProviderGeneric   Provider = "git"
)

// gitHosts are the canonical Git hosts of the cloud providers.
var gitHosts = map[Provider]string{
	ProviderGitHub:    "github.com",
	ProviderGitLab:    "gitlab.com",
	ProviderBitbucket: "bitbucket.org",
}

// providerHosts maps each cloud provider's Git hosts to it: the canonical
// host and the SSH hosts the provider also serves, such as the port-443 SSH
// endpoints and the SSH host Bitbucket's API reports for clone addresses.
var providerHosts = map[string]Provider{
	"github.com":           ProviderGitHub,
	"ssh.github.com":       ProviderGitHub,
	"gitlab.com":           ProviderGitLab,
	"altssh.gitlab.com":    ProviderGitLab,
	"bitbucket.org":        ProviderBitbucket,
	"ssh.bitbucket.org":    ProviderBitbucket,
	"altssh.bitbucket.org": ProviderBitbucket,
}

// ProviderOf returns the provider for host, compared case-insensitively and
// without a user or port.
func ProviderOf(host string) Provider {
	h := strings.ToLower(host)
	if _, rest, ok := strings.Cut(h, "@"); ok {
		h = rest
	}
	if name, _, err := net.SplitHostPort(h); err == nil {
		h = name
	}
	if p, ok := providerHosts[h]; ok {
		return p
	}
	return ProviderGeneric
}

// GitHost returns the canonical Git host of a cloud provider, or "" for a
// generic host.
func (p Provider) GitHost() string {
	return gitHosts[p]
}

// CanonicalHost returns the provider's canonical host for any of its Git
// hosts, so ssh.bitbucket.org and bitbucket.org name one repository, and the
// lowercased host otherwise.
func CanonicalHost(host string) string {
	if p := ProviderOf(host); p != ProviderGeneric {
		return p.GitHost()
	}
	return strings.ToLower(host)
}

// fixedDepth reports whether every repository on the provider sits exactly
// two path segments deep (owner/repo, workspace/repo).
func (p Provider) fixedDepth() bool {
	return p == ProviderGitHub || p == ProviderBitbucket
}

// nestedNamespaces reports whether repositories can sit under nested groups,
// which makes an unmarked path longer than two segments ambiguous.
func (p Provider) nestedNamespaces() bool {
	return p == ProviderGitLab
}
