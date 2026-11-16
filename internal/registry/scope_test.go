package registry_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/registry"
	"github.com/glapsfun/gskill/internal/source"
)

func TestParseScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		provider source.Provider
		path     string
	}{
		{"acme", source.ProviderGitHub, "acme"},
		{"github.com/acme", source.ProviderGitHub, "acme"},
		{"GitHub.com/acme/", source.ProviderGitHub, "acme"},
		{"gitlab.com/acme", source.ProviderGitLab, "acme"},
		{"gitlab.com/acme/platform", source.ProviderGitLab, "acme/platform"},
		{"https://gitlab.com/acme/platform/tools", source.ProviderGitLab, "acme/platform/tools"},
		{"bitbucket.org/acme", source.ProviderBitbucket, "acme"},
		{"https://bitbucket.org/acme-team", source.ProviderBitbucket, "acme-team"},
	}
	for _, tt := range tests {
		got, err := registry.ParseScope(tt.raw)
		if err != nil {
			t.Errorf("ParseScope(%q): %v", tt.raw, err)
			continue
		}
		if got.Provider != tt.provider || got.Path != tt.path || got.Transport != registry.TransportHTTPS {
			t.Errorf("ParseScope(%q) = %+v, want %s %q over https", tt.raw, got, tt.provider, tt.path)
		}
	}
}

func TestParseScope_Refused(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"git.example.com/acme",
		"codeberg.org/acme",
		"github.com",
		"github.com/acme/skills",
		"bitbucket.org/acme/repo",
		"gitlab.com/acme/../x",
		"gitlab.com/acme//x",
		"acme corp",
		"",
	} {
		if _, err := registry.ParseScope(raw); errs.ExitCode(err) != int(errs.CodeUsage) {
			t.Errorf("ParseScope(%q) = %v, want a usage error", raw, err)
		}
	}
	_, err := registry.ParseScope("git.example.com/acme")
	if err == nil || !contains(err.Error(), "github.com, gitlab.com, and bitbucket.org") {
		t.Errorf("unsupported host error %v does not name the supported providers", err)
	}
}

func TestParseTransport(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]registry.Transport{"": registry.TransportHTTPS, "https": registry.TransportHTTPS, "SSH": registry.TransportSSH} {
		if got, err := registry.ParseTransport(raw); err != nil || got != want {
			t.Errorf("ParseTransport(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := registry.ParseTransport("git"); errs.ExitCode(err) != int(errs.CodeUsage) {
		t.Errorf("ParseTransport(git) = %v, want a usage error", err)
	}
}
