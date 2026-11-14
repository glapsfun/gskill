package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/source"
)

// legacyGitLab is how an earlier release recorded the unmarked source
// gitlab.com/acme/skills/tools/lint: repository acme/skills, folder tools/lint.
var legacyGitLab = skillslock.Source{
	Type:     "git",
	Original: "gitlab.com/acme/skills/tools/lint",
	URL:      "https://gitlab.com/acme/skills.git",
	Owner:    "acme",
	Repo:     "skills",
	Path:     "tools/lint",
}

// The "Lock-recorded boundary" table of contracts/source-syntax.md.
func TestParseSource_LockRecordedBoundary(t *testing.T) {
	t.Parallel()

	ref, err := parseSource(legacyGitLab.Original, &legacyGitLab)
	if err != nil {
		t.Fatalf("parseSource with the recorded entry: %v", err)
	}
	if ref.URL != legacyGitLab.URL || ref.Owner != "acme" || ref.Repo != "skills" || ref.Path != "tools/lint" {
		t.Errorf("parseSource = %+v, want the lock's repository and folder", ref)
	}

	if _, err := parseSource(legacyGitLab.Original, nil); !errors.Is(err, source.ErrAmbiguousSource) {
		t.Errorf("parseSource without a lock entry = %v, want ErrAmbiguousSource", err)
	}
	other := legacyGitLab
	other.Original = "gitlab.com/acme/skills//tools/lint"
	if _, err := parseSource(legacyGitLab.Original, &other); !errors.Is(err, source.ErrAmbiguousSource) {
		t.Errorf("parseSource with a different original = %v, want ErrAmbiguousSource", err)
	}
	noURL := legacyGitLab
	noURL.URL = ""
	if _, err := parseSource(legacyGitLab.Original, &noURL); !errors.Is(err, source.ErrAmbiguousSource) {
		t.Errorf("parseSource with no recorded clone address = %v, want ErrAmbiguousSource", err)
	}
}

// Other parse failures are never rescued by the lock.
func TestParseSource_OnlyAmbiguityUsesTheLock(t *testing.T) {
	t.Parallel()

	bad := legacyGitLab
	bad.Original = "acme"
	if _, err := parseSource("acme", &bad); !errors.Is(err, source.ErrInvalidSource) {
		t.Errorf("parseSource(acme) = %v, want ErrInvalidSource", err)
	}
}

func TestRepoFor_NestedAndLegacyGitLab(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name     string
		decl     manifest.Skill
		rec      skillslock.Record
		wantKey  string
		wantPath string
	}{
		{
			name:    "nested group over ssh",
			decl:    manifest.Skill{Name: "lint", Source: "git@gitlab.com:acme/platform/skills.git//tools/lint"},
			wantKey: "gitlab.com/acme/platform/skills", wantPath: "tools/lint",
		},
		{
			name:    "legacy unmarked source keeps its recorded boundary",
			decl:    manifest.Skill{Name: "lint", Source: legacyGitLab.Original},
			rec:     skillslock.Record{Source: legacyGitLab},
			wantKey: "gitlab.com/acme/skills", wantPath: "tools/lint",
		},
	}
	for _, tt := range tests {
		repo, path := repoFor(root, tt.decl, tt.rec)
		if repo.Kind != source.RepoRemote || repo.Key != tt.wantKey || path != tt.wantPath {
			t.Errorf("%s: repo %s %q path %q, want remote %q %q", tt.name, repo.Kind, repo.Key, path, tt.wantKey, tt.wantPath)
		}
	}
}

// HTTPS and SSH spellings of one nested GitLab project form one group, and
// the label GSKILL prints selects it (spec 035 FR-033).
func TestSelectSkills_NestedGitLabGroup(t *testing.T) {
	t.Parallel()

	lf := skillslock.NewState()
	lf.Skills["a"] = skillslock.Record{Source: skillslock.Source{Type: "git", Original: "https://gitlab.com/acme/platform/skills.git"}}
	lf.Skills["b"] = skillslock.Record{Source: skillslock.Source{Type: "git", Original: "git@gitlab.com:acme/platform/skills.git//tools/lint"}}
	lf.Skills["c"] = skillslock.Record{Source: skillslock.Source{Type: "git", Original: "gitlab.com/acme/platform//skills"}}
	for _, spelling := range []string{
		"gitlab.com/acme/platform/skills",
		"git@gitlab.com:acme/platform/skills.git",
		"https://gitlab.com/acme/platform/skills",
	} {
		got, err := selectSkills(t.TempDir(), lf, nil, []string{spelling}, nil)
		if err != nil || strings.Join(got, ",") != "a,b" {
			t.Errorf("--repo %s selected %v, %v; want a,b", spelling, got, err)
		}
	}
}

// Configured repositories name repositories, so an unmarked nested GitLab
// entry is the whole repository; every other entry passes through.
func TestRepositorySources(t *testing.T) {
	t.Parallel()

	got := repositorySources([]string{"gitlab.com/acme/platform/skills", "gitlab.com/acme/platform/skills/", "acme/skills/tools", "./local"})
	want := []string{"gitlab.com/acme/platform/skills.git", "gitlab.com/acme/platform/skills.git", "acme/skills/tools", "./local"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("repositorySources = %v, want %v", got, want)
	}
}
