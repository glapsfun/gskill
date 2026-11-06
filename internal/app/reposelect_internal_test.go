package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/source"
)

// The repository a skill is grouped under comes from the source the update
// would fetch from: the declaration when it parses, else the lock (spec 032
// FR-004).
func TestRepoFor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(filepath.Dir(root), "repo", ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	gitlab := skillslock.Record{Source: skillslock.Source{Type: "git", Original: "gitlab.com/acme/skills"}}

	tests := []struct {
		name     string
		decl     manifest.Skill
		rec      skillslock.Record
		wantKind source.RepoKind
		wantKey  string
		wantPath string
	}{
		{
			name: "declaration overrides the lock", decl: manifest.Skill{Source: "acme/skills"}, rec: gitlab,
			wantKind: source.RepoRemote, wantKey: "github.com/acme/skills",
		},
		{
			name: "unparseable declaration falls back to the lock", decl: manifest.Skill{Source: "%%%"}, rec: gitlab,
			wantKind: source.RepoRemote, wantKey: "gitlab.com/acme/skills",
		},
		{
			name:     "lock type is never read",
			decl:     manifestSkillFrom("x", skillslock.Record{Source: skillslock.Source{Type: "github", Original: "acme/skills"}}),
			rec:      skillslock.Record{Source: skillslock.Source{Type: "github", Original: "acme/skills"}},
			wantKind: source.RepoRemote, wantKey: "github.com/acme/skills",
		},
		{
			name: "local git repository stays local", decl: manifest.Skill{Source: "../repo"},
			wantKind: source.RepoLocal, wantKey: filepath.Join(filepath.Dir(root), "repo"),
		},
		{
			name: "declared skill path", decl: manifest.Skill{Name: "lint", Source: "acme/skills", Skill: "tools/lint"},
			wantKind: source.RepoRemote, wantKey: "github.com/acme/skills", wantPath: "tools/lint",
		},
		{
			name: "path in the source", decl: manifest.Skill{Name: "lint", Source: "acme/skills/tools/lint"},
			wantKind: source.RepoRemote, wantKey: "github.com/acme/skills", wantPath: "tools/lint",
		},
		{
			name: "path equal to the name adds nothing", decl: manifest.Skill{Name: "lint", Source: "acme/skills/lint"},
			wantKind: source.RepoRemote, wantKey: "github.com/acme/skills",
		},
	}
	for _, tt := range tests {
		repo, path := repoFor(root, tt.decl, tt.rec)
		if repo.Kind != tt.wantKind || repo.Key != tt.wantKey {
			t.Errorf("%s: repo = %s %q, want %s %q", tt.name, repo.Kind, repo.Key, tt.wantKind, tt.wantKey)
		}
		if path != tt.wantPath {
			t.Errorf("%s: path = %q, want %q", tt.name, path, tt.wantPath)
		}
	}
}

func TestRepoFor_UnparseableSourceIsInvalid(t *testing.T) {
	t.Parallel()

	repo, _ := repoFor(t.TempDir(), manifest.Skill{}, skillslock.Record{Source: skillslock.Source{Original: "%%%"}})
	if repo.Kind != source.RepoInvalid || repo.Label != "%%%" {
		t.Errorf("repo = %+v, want invalid %q", repo, "%%%")
	}
}

// selectLock tracks two skills from github.com/acme/skills, one from
// gitlab.com/acme/skills, and one local directory.
func selectLock() *skillslock.State {
	lf := skillslock.NewState()
	lf.Skills["code-review"] = skillslock.Record{Source: skillslock.Source{Type: "git", Original: "https://github.com/acme/skills.git"}}
	lf.Skills["kubernetes"] = skillslock.Record{Source: skillslock.Source{Type: "git", Original: "acme/skills"}}
	lf.Skills["gl"] = skillslock.Record{Source: skillslock.Source{Type: "git", Original: "gitlab.com/acme/skills"}}
	lf.Skills["notes"] = skillslock.Record{Source: skillslock.Source{Type: "local", Original: "../tools"}}
	return lf
}

func TestSelectSkills_Selection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name         string
		repos, names []string
		want         string
	}{
		{name: "nothing selects every tracked skill", want: "code-review,gl,kubernetes,notes"},
		{name: "names alone", names: []string{"kubernetes"}, want: "kubernetes"},
		{name: "one repository", repos: []string{"acme/skills"}, want: "code-review,kubernetes"},
		{name: "two repositories", repos: []string{"acme/skills", "../tools"}, want: "code-review,kubernetes,notes"},
		{name: "repository and a name", repos: []string{"acme/skills"}, names: []string{"kubernetes"}, want: "kubernetes"},
		{name: "repeated value", repos: []string{"acme/skills", "github.com/acme/skills"}, want: "code-review,kubernetes"},
	}
	for _, tt := range tests {
		got, err := selectSkills(root, selectLock(), nil, tt.repos, tt.names)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if strings.Join(got, ",") != tt.want {
			t.Errorf("%s: selected %v, want %s", tt.name, got, tt.want)
		}
	}
}

// Every accepted spelling of a repository selects the same skills, and a
// same-named repository on another host never does (spec 032 SC-003).
func TestSelectSkills_SpellingsSelectTheSameGroup(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{
		"acme/skills",
		"github.com/acme/skills",
		"https://github.com/acme/skills",
		"https://github.com/acme/skills.git",
		"git@github.com:acme/skills.git",
	} {
		got, err := selectSkills(t.TempDir(), selectLock(), nil, []string{spelling}, nil)
		if err != nil || strings.Join(got, ",") != "code-review,kubernetes" {
			t.Errorf("--repo %s selected %v, %v; want code-review,kubernetes", spelling, got, err)
		}
	}
}

func TestSelectSkills_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		repos, names []string
		code         int
		msg, hint    string
	}{
		{
			name: "path inside a repository", repos: []string{"acme/skills/tools"}, code: 2,
			msg:  `--repo "acme/skills/tools" names a path inside a repository`,
			hint: "use --repo github.com/acme/skills and select skills by name",
		},
		{
			name: "unparseable value", repos: []string{"acme"}, code: 2,
			msg: `--repo "acme": invalid source: "acme" is not owner/repo`,
		},
		{
			name: "no tracked skill", repos: []string{"github.com/nobody/nothing"}, code: 2,
			msg:  `no tracked skill comes from "github.com/nobody/nothing"`,
			hint: "repositories in this project: ../tools, github.com/acme/skills, gitlab.com/acme/skills",
		},
		{
			name: "named skill in another group", repos: []string{"acme/skills"}, names: []string{"gl"}, code: 2,
			msg: `skill "gl" comes from gitlab.com/acme/skills, not from the selected repository`,
		},
		{
			name: "named skill not tracked", repos: []string{"acme/skills"}, names: []string{"nope"}, code: 3,
			msg: `invalid lock: skill "nope" is not declared`,
		},
		{
			name: "path errors come first", repos: []string{"github.com/nobody/nothing", "acme/skills/tools"}, code: 2,
			msg: `--repo "acme/skills/tools" names a path inside a repository`,
		},
		{
			name: "unknown repositories before named skills", repos: []string{"github.com/nobody/nothing"}, names: []string{"gl"}, code: 2,
			msg: `no tracked skill comes from "github.com/nobody/nothing"`,
		},
	}
	for _, tt := range tests {
		_, err := selectSkills(t.TempDir(), selectLock(), nil, tt.repos, tt.names)
		if err == nil {
			t.Errorf("%s: no error", tt.name)
			continue
		}
		if got := errs.ExitCode(err); got != tt.code {
			t.Errorf("%s: exit %d, want %d (%v)", tt.name, got, tt.code, err)
		}
		if err.Error() != tt.msg {
			t.Errorf("%s: message %q, want %q", tt.name, err.Error(), tt.msg)
		}
		if tt.hint != "" && errs.HintOf(err) != tt.hint {
			t.Errorf("%s: hint %q, want %q", tt.name, errs.HintOf(err), tt.hint)
		}
	}
}
