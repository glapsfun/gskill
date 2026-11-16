package source_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/source"
)

func TestProviderOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host string
		want source.Provider
	}{
		{"github.com", source.ProviderGitHub},
		{"GitHub.com", source.ProviderGitHub},
		{"gitlab.com", source.ProviderGitLab},
		{"gitlab.com:2222", source.ProviderGitLab},
		{"git@bitbucket.org", source.ProviderBitbucket},
		{"bitbucket.org", source.ProviderBitbucket},
		{"ssh.bitbucket.org", source.ProviderBitbucket},
		{"altssh.bitbucket.org:443", source.ProviderBitbucket},
		{"altssh.gitlab.com", source.ProviderGitLab},
		{"ssh.github.com:443", source.ProviderGitHub},
		{"evil.github.com.example", source.ProviderGeneric},
		{"github-work", source.ProviderGeneric},
		{"codeberg.org", source.ProviderGeneric},
		{"git.example.com", source.ProviderGeneric},
		{"", source.ProviderGeneric},
	}
	for _, tt := range tests {
		if got := source.ProviderOf(tt.host); got != tt.want {
			t.Errorf("ProviderOf(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestProviderGitHost(t *testing.T) {
	t.Parallel()

	for p, want := range map[source.Provider]string{
		source.ProviderGitHub:    "github.com",
		source.ProviderGitLab:    "gitlab.com",
		source.ProviderBitbucket: "bitbucket.org",
		source.ProviderGeneric:   "",
	} {
		if got := p.GitHost(); got != want {
			t.Errorf("%s.GitHost() = %q, want %q", p, got, want)
		}
	}
}
