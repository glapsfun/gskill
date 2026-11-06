package source_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/source"
)

// repoOf parses raw and returns its repository identity against root.
func repoOf(t *testing.T, raw, root string) source.Repo {
	t.Helper()
	ref, err := source.Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%q): %v", raw, err)
	}
	return source.RepoOf(ref, root)
}

func TestRepoOf_EqualityTable(t *testing.T) {
	t.Parallel()

	p := t.TempDir()
	app := filepath.Join(p, "app")
	tests := []struct {
		raw   string
		root  string
		kind  source.RepoKind
		key   string
		group string
	}{
		{"acme/skills", p, source.RepoRemote, "github.com/acme/skills", "A"},
		{"github.com/acme/skills", p, source.RepoRemote, "github.com/acme/skills", "A"},
		{"https://github.com/acme/skills", p, source.RepoRemote, "github.com/acme/skills", "A"},
		{"https://github.com/acme/skills.git", p, source.RepoRemote, "github.com/acme/skills", "A"},
		{"git@github.com:acme/skills.git", p, source.RepoRemote, "github.com/acme/skills", "A"},
		{"https://GitHub.com/Acme/Skills", p, source.RepoRemote, "github.com/acme/skills", "A"},
		{"gitlab.com/acme/skills", p, source.RepoRemote, "gitlab.com/acme/skills", "B"},
		{"github.com/acme-labs/skills", p, source.RepoRemote, "github.com/acme-labs/skills", "C"},
		{"https://git.example.com/Team/Skills.git", p, source.RepoRemote, "git.example.com/Team/Skills", "D"},
		{"https://git.example.com/team/skills.git", p, source.RepoRemote, "git.example.com/team/skills", "E"},
		{"./tools", p, source.RepoLocal, filepath.Join(p, "tools"), "F"},
		{"./tools/", p, source.RepoLocal, filepath.Join(p, "tools"), "F"},
		{filepath.Join(p, "tools"), p, source.RepoLocal, filepath.Join(p, "tools"), "F"},
		{"../tools", app, source.RepoLocal, filepath.Join(p, "tools"), "F"},
	}

	got := make([]source.Repo, len(tests))
	for i, tt := range tests {
		got[i] = repoOf(t, tt.raw, tt.root)
		if got[i].Kind != tt.kind || got[i].Key != tt.key {
			t.Errorf("RepoOf(%q) = %s %q, want %s %q", tt.raw, got[i].Kind, got[i].Key, tt.kind, tt.key)
		}
	}
	for i := range tests {
		for j := range tests {
			if want := tests[i].group == tests[j].group; got[i].Equal(got[j]) != want {
				t.Errorf("Equal(%q, %q) = %v, want %v", tests[i].raw, tests[j].raw, !want, want)
			}
		}
	}
}

func TestRepoOf_LocalGitRepoStaysLocal(t *testing.T) {
	t.Parallel()

	p := t.TempDir()
	if err := os.MkdirAll(filepath.Join(p, "tools", ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if r := repoOf(t, "./tools", p); r.Kind != source.RepoLocal || r.Key != filepath.Join(p, "tools") {
		t.Errorf("local git repo = %s %q, want local %q", r.Kind, r.Key, filepath.Join(p, "tools"))
	}
}

func TestRepoOf_InRepoPathsShareTheRepository(t *testing.T) {
	t.Parallel()

	lint := repoOf(t, "acme/skills/tools/lint", "/p")
	writing := repoOf(t, "acme/skills/writing", "/p")
	if !lint.Equal(writing) || lint.Key != "github.com/acme/skills" {
		t.Errorf("in-repo paths split the group: %q vs %q", lint.Key, writing.Key)
	}
}

func TestRepoOf_Labels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw, label, key string
	}{
		{"https://GitHub.com/Acme/Skills", "github.com/Acme/Skills", "github.com/acme/skills"},
		{"../tools", "../tools", ""},
		{"./tools/", "./tools", ""},
		{"https://example.com/skills/", "https://example.com/skills/", "https://example.com/skills"},
		{"https://Example.com/skills.git/", "https://Example.com/skills.git/", "https://example.com/skills"},
	}
	for _, tt := range tests {
		r := repoOf(t, tt.raw, "/p")
		if r.Label != tt.label {
			t.Errorf("RepoOf(%q).Label = %q, want %q", tt.raw, r.Label, tt.label)
		}
		if tt.key != "" && r.Key != tt.key {
			t.Errorf("RepoOf(%q).Key = %q, want %q", tt.raw, r.Key, tt.key)
		}
	}
}

func TestRepoOf_HomeIsExpanded(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if r := repoOf(t, "~/skills", "/p"); r.Key != filepath.Join(home, "skills") {
		t.Errorf("RepoOf(~/skills).Key = %q, want %q", r.Key, filepath.Join(home, "skills"))
	}
}

func TestRepoOf_UnparsedSourceIsInvalid(t *testing.T) {
	t.Parallel()

	r := source.RepoOf(source.Ref{Original: "%%bad"}, "/p")
	if r.Kind != source.RepoInvalid || r.Key != "%%bad" || r.Label != "%%bad" {
		t.Errorf("RepoOf(zero ref) = %+v, want invalid %%%%bad", r)
	}
}

func TestParseRepo_MatchesRepoOf(t *testing.T) {
	t.Parallel()

	want := repoOf(t, "https://github.com/acme/skills.git", "/p")
	for _, raw := range []string{
		"acme/skills",
		"github.com/acme/skills",
		"https://github.com/acme/skills",
		"https://github.com/acme/skills.git",
		"git@github.com:acme/skills.git",
		"https://GitHub.com/Acme/Skills",
	} {
		got, err := source.ParseRepo(raw, "/p")
		if err != nil {
			t.Errorf("ParseRepo(%q): %v", raw, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("ParseRepo(%q) = %q, want %q", raw, got.Key, want.Key)
		}
	}
	local, err := source.ParseRepo("./tools", "/p")
	if err != nil || local.Kind != source.RepoLocal || local.Key != "/p/tools" {
		t.Errorf("ParseRepo(./tools) = %+v, %v; want local /p/tools", local, err)
	}
}

func TestParseRepo_RejectsPathsInsideARepository(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"acme/skills/tools",
		"github.com/acme/skills/tools",
		"https://github.com/acme/skills/tree/main/tools",
		"git@github.com:acme/skills/tools.git",
		"https://GitHub.com/acme/skills/tools",
	} {
		_, err := source.ParseRepo(raw, "/p")
		var pe *source.RepoPathError
		if !errors.As(err, &pe) {
			t.Errorf("ParseRepo(%q) error = %v, want a *RepoPathError", raw, err)
			continue
		}
		if pe.Value != raw || pe.Repo != "github.com/acme/skills" {
			t.Errorf("ParseRepo(%q) = %+v, want Value %q and Repo github.com/acme/skills", raw, pe, raw)
		}
		if want := `--repo "` + raw + `" names a path inside a repository`; pe.Error() != want {
			t.Errorf("Error() = %q, want %q", pe.Error(), want)
		}
	}
}

func TestParseRepo_RejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"acme", "", "   "} {
		_, err := source.ParseRepo(raw, "/p")
		if err == nil {
			t.Errorf("ParseRepo(%q) succeeded, want an error", raw)
			continue
		}
		var pe *source.RepoPathError
		if errors.As(err, &pe) {
			t.Errorf("ParseRepo(%q) returned a path error, want a parse error: %v", raw, err)
		}
		if !errors.Is(err, source.ErrInvalidSource) {
			t.Errorf("ParseRepo(%q) error %v does not wrap ErrInvalidSource", raw, err)
		}
	}
}
