package source_test

import (
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/source"
)

func TestParse_GitForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantOwner string
		wantRepo  string
		wantPath  string
		wantURL   string
	}{
		{
			name:      "host shorthand with in-repo path",
			raw:       "github.com/acme/widgets/my-skill",
			wantOwner: "acme", wantRepo: "widgets", wantPath: "my-skill",
			wantURL: "https://github.com/acme/widgets.git",
		},
		{
			name:      "bare owner/repo defaults to github",
			raw:       "acme/widgets",
			wantOwner: "acme", wantRepo: "widgets", wantPath: "",
			wantURL: "https://github.com/acme/widgets.git",
		},
		{
			name:      "full https with .git",
			raw:       "https://github.com/acme/widgets.git",
			wantOwner: "acme", wantRepo: "widgets", wantPath: "",
			wantURL: "https://github.com/acme/widgets.git",
		},
		{
			name:      "full https without .git and subpath",
			raw:       "https://github.com/acme/widgets/sub/skill",
			wantOwner: "acme", wantRepo: "widgets", wantPath: "sub/skill",
			wantURL: "https://github.com/acme/widgets.git",
		},
		{
			name:      "ssh scp-like form preserved",
			raw:       "git@github.com:acme/widgets.git",
			wantOwner: "acme", wantRepo: "widgets", wantPath: "",
			wantURL: "git@github.com:acme/widgets.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ref, err := source.Parse(tt.raw)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.raw, err)
			}
			if ref.Type != source.TypeGit {
				t.Errorf("Type = %q, want git", ref.Type)
			}
			if ref.Owner != tt.wantOwner || ref.Repo != tt.wantRepo {
				t.Errorf("owner/repo = %q/%q, want %q/%q", ref.Owner, ref.Repo, tt.wantOwner, tt.wantRepo)
			}
			if ref.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", ref.Path, tt.wantPath)
			}
			if ref.URL != tt.wantURL {
				t.Errorf("url = %q, want %q", ref.URL, tt.wantURL)
			}
			if ref.Original != tt.raw {
				t.Errorf("original = %q, want %q", ref.Original, tt.raw)
			}
		})
	}
}

func TestParse_LocalForms(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"./local/skill", "../sibling", "/abs/path"} {
		ref, err := source.Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%q): %v", raw, err)
		}
		if ref.Type != source.TypeLocal {
			t.Errorf("Parse(%q) Type = %q, want local", raw, ref.Type)
		}
		if ref.LocalPath == "" {
			t.Errorf("Parse(%q) LocalPath empty", raw)
		}
	}
}

func TestParse_NonGitURLIsURLType(t *testing.T) {
	t.Parallel()

	ref, err := source.Parse("https://example.com/bundle.tar.gz")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ref.Type != source.TypeURL {
		t.Errorf("Type = %q, want url", ref.Type)
	}
}

func TestParse_Invalid(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "   ", "single-segment"} {
		if _, err := source.Parse(raw); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", raw)
		} else if !errors.Is(err, source.ErrInvalidSource) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalidSource", raw, err)
		}
	}
}

func TestSourceRef_Identity(t *testing.T) {
	t.Parallel()

	ref, err := source.Parse("github.com/acme/widgets/my-skill")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := ref.Identity(), "github.com/acme/widgets/my-skill"; got != want {
		t.Errorf("Identity() = %q, want %q", got, want)
	}
}

func TestRefDisplay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ref  source.Ref
		want string
	}{
		{
			name: "owner and repo",
			ref:  source.Ref{Type: source.TypeGit, URL: "https://github.com/acme/skills.git", Owner: "acme", Repo: "skills"},
			want: "acme/skills",
		},
		{
			name: "promoted local git has repo only",
			ref:  source.Ref{Type: source.TypeGit, URL: "/abs/path/tools", Repo: "tools"},
			want: "tools",
		},
		{
			name: "bare url trims empty segments",
			ref:  source.Ref{Type: source.TypeGit, URL: "https://example.com/skills"},
			want: "example.com",
		},
		{
			name: "local path passes through untrimmed",
			ref:  source.Ref{Type: source.TypeLocal, LocalPath: "/Users/x/skills"},
			want: "/Users/x/skills",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.ref.Display(); got != tc.want {
				t.Errorf("Display() = %q, want %q", got, tc.want)
			}
		})
	}
}
