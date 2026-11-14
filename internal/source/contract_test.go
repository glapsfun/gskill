package source_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
)

// TestParse_ContractAccepted is the "Accepted" table of
// specs/035-provider-private-sources/contracts/source-syntax.md.
func TestParse_ContractAccepted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw, url, owner, repo, path string
	}{
		{"acme/skills", "https://github.com/acme/skills.git", "acme", "skills", ""},
		{"acme/skills/tools/lint", "https://github.com/acme/skills.git", "acme", "skills", "tools/lint"},
		{"github.com/acme/skills/tools/lint", "https://github.com/acme/skills.git", "acme", "skills", "tools/lint"},
		{"https://github.com/acme/skills.git", "https://github.com/acme/skills.git", "acme", "skills", ""},
		{"git@github.com:acme/skills.git", "git@github.com:acme/skills.git", "acme", "skills", ""},
		{"git@github.com:acme/skills.git//tools/lint", "git@github.com:acme/skills.git", "acme", "skills", "tools/lint"},
		{"git@github.com:acme/skills/tools/lint", "git@github.com:acme/skills.git", "acme", "skills", "tools/lint"},
		{"ssh://git@github.com/acme/skills.git", "ssh://git@github.com/acme/skills.git", "acme", "skills", ""},
		{"http://github.com/acme/skills", "https://github.com/acme/skills.git", "acme", "skills", ""},
		{"git://github.com/acme/skills.git", "https://github.com/acme/skills.git", "acme", "skills", ""},
		{"https://gitlab.com/acme/skills", "https://gitlab.com/acme/skills.git", "acme", "skills", ""},
		{"https://gitlab.com/acme/platform/skills.git", "https://gitlab.com/acme/platform/skills.git", "acme/platform", "skills", ""},
		{"git@gitlab.com:acme/platform/skills.git", "git@gitlab.com:acme/platform/skills.git", "acme/platform", "skills", ""},
		{"gitlab.com/acme/platform/skills//tools/lint", "https://gitlab.com/acme/platform/skills.git", "acme/platform", "skills", "tools/lint"},
		{"gitlab.com/acme/platform/skills.git/tools/lint", "https://gitlab.com/acme/platform/skills.git", "acme/platform", "skills", "tools/lint"},
		{"https://gitlab.com/acme/platform/skills.git//tools/lint", "https://gitlab.com/acme/platform/skills.git", "acme/platform", "skills", "tools/lint"},
		{"ssh://git@gitlab.com:2222/acme/platform/skills.git", "ssh://git@gitlab.com:2222/acme/platform/skills.git", "acme/platform", "skills", ""},
		{"https://bitbucket.org/acme/skills.git", "https://bitbucket.org/acme/skills.git", "acme", "skills", ""},
		{"https://ci@bitbucket.org/acme/skills.git", "https://ci@bitbucket.org/acme/skills.git", "acme", "skills", ""},
		{"git@bitbucket.org:acme/skills.git", "git@bitbucket.org:acme/skills.git", "acme", "skills", ""},
		{"bitbucket.org/acme/skills/tools/lint", "https://bitbucket.org/acme/skills.git", "acme", "skills", "tools/lint"},
		{"deploy@git.example.com:team/sub/skills.git", "deploy@git.example.com:team/sub/skills.git", "team/sub", "skills", ""},
		{"git@github-work:acme/skills.git", "git@github-work:acme/skills.git", "acme", "skills", ""},
		{"git@ssh.bitbucket.org:acme/skills.git", "git@ssh.bitbucket.org:acme/skills.git", "acme", "skills", ""},
		{"https://git.example.com/team/sub/skills.git//x", "https://git.example.com/team/sub/skills.git", "team/sub", "skills", "x"},
		{"https://git.example.com:8443/team/skills.git", "https://git.example.com:8443/team/skills.git", "team", "skills", ""},
		{"git.example.com/team/skills/x", "https://git.example.com/team/skills.git", "team", "skills", "x"},
		{"git@git.example.com:/srv/git/skills.git", "git@git.example.com:/srv/git/skills.git", "srv/git", "skills", ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			ref, err := source.Parse(tt.raw)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.raw, err)
			}
			got := []string{string(ref.Type), ref.URL, ref.Owner, ref.Repo, ref.Path, ref.Original}
			want := []string{string(source.TypeGit), tt.url, tt.owner, tt.repo, tt.path, tt.raw}
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("Parse(%q)\n got  type|url|owner|repo|path|original = %v\n want %v", tt.raw, got, want)
			}
		})
	}
}

// TestParse_ContractRefused is the "Refused" table of contracts/source-syntax.md.
func TestParse_ContractRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		want     error
		exit     int
		contains []string
	}{
		{
			"gitlab.com/acme/platform/skills", source.ErrAmbiguousSource, 2,
			[]string{`"gitlab.com/acme/platform/skills.git"`, `"gitlab.com/acme/platform//skills"`},
		},
		{
			"https://gitlab.com/acme/platform/skills", source.ErrAmbiguousSource, 2,
			[]string{`"https://gitlab.com/acme/platform/skills.git"`, `"https://gitlab.com/acme/platform//skills"`},
		},
		{
			"git@gitlab.com:acme/platform/skills", source.ErrAmbiguousSource, 2,
			[]string{`"git@gitlab.com:acme/platform/skills.git"`, `"git@gitlab.com:acme/platform//skills"`},
		},
		{"https://user:secret@gitlab.com/acme/skills.git", source.ErrCredentialInSource, 2, nil},
		{"https://ghp_abc123@github.com/acme/skills.git", source.ErrCredentialInSource, 2, nil},
		{"https://oauth2:glpat-x@gitlab.com/acme/skills.git", source.ErrCredentialInSource, 2, nil},
		{"https://x-token-auth:ATCTT3xyz@bitbucket.org/acme/skills.git", source.ErrCredentialInSource, 2, nil},
		{"ssh://deploy:secret@git.example.com/team/skills.git", source.ErrCredentialInSource, 2, nil},
		{"deploy:secret@git.example.com:team/skills.git", source.ErrCredentialInSource, 2, nil},
		{"github.com/acme//", source.ErrInvalidSource, 1, nil},
		{"github.com/acme/skills//", source.ErrInvalidSource, 1, nil},
		{"git@gitlab.com:acme", source.ErrInvalidSource, 1, nil},
		{"github.com/acme/team/skills//tools", source.ErrInvalidSource, 1, nil},
		{"github.com/acme/skills/../x", source.ErrInvalidSource, 1, nil},
		{"ssh://git@git.example.com/skills", source.ErrInvalidSource, 1, nil},
		{
			"https://gitlab.com/acme/skills/-/tree/main/tools/lint", source.ErrInvalidSource, 1,
			[]string{"is a GitLab web address", `"https://gitlab.com/acme/skills.git//tools/lint"`},
		},
		{
			"gitlab.com/acme/platform/skills/-/blob/main/lint/SKILL.md", source.ErrInvalidSource, 1,
			[]string{`"gitlab.com/acme/platform/skills.git//lint"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			_, err := source.Parse(tt.raw)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Parse(%q) error = %v, want %v", tt.raw, err, tt.want)
			}
			if got := errs.ExitCode(err); got != tt.exit {
				t.Errorf("exit code = %d, want %d", got, tt.exit)
			}
			for _, s := range tt.contains {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q does not contain %s", err, s)
				}
			}
			if errors.Is(tt.want, source.ErrCredentialInSource) {
				for _, secret := range []string{"secret", "ghp_abc123", "glpat-x", "ATCTT3xyz", "oauth2", "user:"} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("error %q repeats %q", err, secret)
					}
				}
			}
		})
	}
}

func TestRef_IdentityAcrossSpellings(t *testing.T) {
	t.Parallel()

	same := [][]string{
		{"https://gitlab.com/acme/skills.git", "git@gitlab.com:acme/skills.git", "ssh://git@GitLab.com:22/acme/skills", "gitlab.com/acme/skills"},
		{"git@gitlab.com:acme/platform/skills.git", "https://gitlab.com/acme/platform/skills.git", "gitlab.com/acme/platform/skills//x"},
		// Bitbucket's API reports SSH clone addresses on ssh.bitbucket.org.
		{"https://bitbucket.org/acme/skills.git", "git@ssh.bitbucket.org:acme/skills.git", "ssh://git@altssh.bitbucket.org:443/acme/skills.git"},
		{"github.com/acme/skills", "ssh://git@ssh.github.com:443/acme/skills.git"},
	}
	for _, group := range same {
		first := repoOf(t, group[0], "/p")
		for _, raw := range group[1:] {
			if got := repoOf(t, raw, "/p"); !got.Equal(first) {
				t.Errorf("RepoOf(%q) = %q, want the same repository as %q (%q)", raw, got.Key, group[0], first.Key)
			}
		}
	}

	different := [][2]string{
		{"github.com/acme/skills", "gitlab.com/acme/skills"},
		{"git@github-work:acme/skills.git", "github.com/acme/skills"},
		{"gitlab.com/acme/skills", "bitbucket.org/acme/skills"},
	}
	for _, pair := range different {
		if repoOf(t, pair[0], "/p").Equal(repoOf(t, pair[1], "/p")) {
			t.Errorf("%q and %q compare equal, want different repositories", pair[0], pair[1])
		}
	}
}

func TestRef_HostProviderIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw, host string
		provider  source.Provider
		identity  string
	}{
		{"acme/skills/x", "github.com", source.ProviderGitHub, "github.com/acme/skills/x"},
		{"ssh://deploy@GitLab.com:2222/acme/platform/skills.git", "gitlab.com", source.ProviderGitLab, "gitlab.com/acme/platform/skills"},
		{"https://ci@bitbucket.org/acme/skills.git", "bitbucket.org", source.ProviderBitbucket, "bitbucket.org/acme/skills"},
		{"git@github-work:acme/skills.git", "github-work", source.ProviderGeneric, "github-work/acme/skills"},
	}
	for _, tt := range tests {
		ref, err := source.Parse(tt.raw)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.raw, err)
		}
		if ref.Host() != tt.host || ref.Provider() != tt.provider || ref.Identity() != tt.identity {
			t.Errorf("Parse(%q): host %q provider %q identity %q, want %q %q %q",
				tt.raw, ref.Host(), ref.Provider(), ref.Identity(), tt.host, tt.provider, tt.identity)
		}
	}
}

func TestRef_DisplayNamesHostOffGitHub(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"acme/skills/tools/lint":                  "acme/skills",
		"git@github.com:acme/skills.git":          "acme/skills",
		"git@gitlab.com:acme/platform/skills.git": "gitlab.com/acme/platform/skills",
		"https://bitbucket.org/acme/skills.git":   "bitbucket.org/acme/skills",
		"git@github-work:acme/skills.git":         "github-work/acme/skills",
		"git@ssh.bitbucket.org:acme/skills.git":   "bitbucket.org/acme/skills",
	}
	for raw, want := range tests {
		ref, err := source.Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%q): %v", raw, err)
		}
		if got := ref.Display(); got != want {
			t.Errorf("Display(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseRepo_NestedGitLabNamesTheRepository(t *testing.T) {
	t.Parallel()

	want := repoOf(t, "https://gitlab.com/acme/platform/skills.git", "/p")
	for _, raw := range []string{
		"gitlab.com/acme/platform/skills",
		"https://gitlab.com/acme/platform/skills",
		"git@gitlab.com:acme/platform/skills",
		"git@gitlab.com:acme/platform/skills.git",
		"gitlab.com/acme/platform/skills/",
	} {
		got, err := source.ParseRepo(raw, "/p")
		if err != nil {
			t.Errorf("ParseRepo(%q): %v", raw, err)
			continue
		}
		if !got.Equal(want) || got.Label != "gitlab.com/acme/platform/skills" {
			t.Errorf("ParseRepo(%q) = %q (%q), want %q", raw, got.Key, got.Label, want.Key)
		}
	}

	_, err := source.ParseRepo("gitlab.com/acme/platform/skills//tools/lint", "/p")
	var pe *source.RepoPathError
	if !errors.As(err, &pe) || pe.Repo != "gitlab.com/acme/platform/skills" {
		t.Errorf("ParseRepo with a folder = %v, want a *RepoPathError for gitlab.com/acme/platform/skills", err)
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()

	tests := map[string]string{ //nolint:gosec // fake credentials, the point of a redaction test
		"https://user:secret@gitlab.com/acme/skills.git": "https://***@gitlab.com/acme/skills.git",
		"https://ghp_abc@github.com/acme/skills.git":     "https://***@github.com/acme/skills.git",
		"deploy:secret@git.example.com:team/skills.git":  "***@git.example.com:team/skills.git",
		"https://user:p@ss@gitlab.com/acme/skills.git":   "https://***@gitlab.com/acme/skills.git",
		"https://ci@bitbucket.org/acme/skills.git":       "https://ci@bitbucket.org/acme/skills.git",
		"git@github.com:acme/skills.git":                 "git@github.com:acme/skills.git",
		"acme/skills":                                    "acme/skills",
		"./local/skills":                                 "./local/skills",
	}
	for raw, want := range tests {
		if got := source.Redact(raw); got != want {
			t.Errorf("Redact(%q) = %q, want %q", raw, got, want)
		}
	}
	// Leading whitespace does not hide user info from redaction.
	if got := source.Redact(" https://user:secret@gitlab.com/acme/skills"); got != " https://***@gitlab.com/acme/skills" {
		t.Errorf("Redact with leading space = %q", got)
	}
}
