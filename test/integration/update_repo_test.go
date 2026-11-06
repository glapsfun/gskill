package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/testutil"
)

// repoFixture is a project tracking three local git repositories:
//   - alpha: lint (^1.0.0) and docs (exact 1.0.0), v1.1.0 published
//   - beta: deploy (^1.0.0), v1.1.0 published
//   - gamma: pinned (exact 1.0.0), v1.1.0 published
type repoFixture struct {
	proj, alpha, beta, gamma string
}

func newRepoFixture(t *testing.T) repoFixture {
	t.Helper()
	f := repoFixture{proj: newProject(t)}
	initProject(t, f.proj)

	f.alpha = testutil.InitSkillRepo(t, "lint", validSkill("lint"))
	testutil.PublishCommit(t, f.alpha, "docs", validSkill("docs"))
	gitRun(t, f.alpha, "tag", "v1.0.0")
	f.beta = gitRepo(t, validSkill("deploy"), "v1.0.0")
	f.gamma = gitRepo(t, validSkill("pinned"), "v1.0.0")

	for _, add := range [][]string{
		{"add", f.alpha, "--skill", "lint", "--version", "^1.0.0"},
		{"add", f.alpha, "--skill", "docs", "--version", "1.0.0"},
		{"add", f.beta, "--version", "^1.0.0"},
		{"add", f.gamma, "--version", "1.0.0"},
	} {
		if _, stderr, code := runGskill(t, f.proj, add...); code != 0 {
			t.Fatalf("%v: exit %d: %s", add, code, stderr)
		}
	}

	testutil.PublishCommit(t, f.alpha, "lint", testutil.SkillBody("lint", "v1.1.0"))
	testutil.PublishCommit(t, f.alpha, "docs", testutil.SkillBody("docs", "v1.1.0"))
	gitRun(t, f.alpha, "tag", "v1.1.0")
	publishNewVersion(t, f.beta, "deploy", "v1.1.0")
	publishNewVersion(t, f.gamma, "pinned", "v1.1.0")
	return f
}

// record reads one skill's lock record.
func (f repoFixture) record(t *testing.T, name string) skillslock.Record {
	t.Helper()
	l, err := skillslock.Load(filepath.Join(f.proj, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := l.Entry(name)
	if !ok {
		t.Fatalf("lock has no entry %q", name)
	}
	return skillslock.ToRecord(name, e)
}

func (f repoFixture) version(t *testing.T, name string) string {
	t.Helper()
	return f.record(t, name).Resolved.Version
}

// files snapshots the project files an update may write.
func (f repoFixture) files(t *testing.T) string {
	t.Helper()
	return string(readFile(t, filepath.Join(f.proj, "skills-lock.json"))) +
		string(readFile(t, filepath.Join(f.proj, "skills.toml")))
}

func (f repoFixture) wantVersions(t *testing.T, want map[string]string) {
	t.Helper()
	for name, v := range want {
		if got := f.version(t, name); got != v {
			t.Errorf("%s locked at %q, want %q", name, got, v)
		}
	}
}

// --repo updates only the selected group, leaves its pin in place, and
// never touches another group (spec 032 US3 scenario 1, SC-004).
func TestUpdateRepo_UpdatesOnlyTheGroup(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	deployBefore := f.record(t, "deploy")
	manifestBefore := readFile(t, filepath.Join(f.proj, "skills.toml"))

	stdout, stderr, code := runGskill(t, f.proj, "--no-interactive", "update", "--repo", f.alpha)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	f.wantVersions(t, map[string]string{"lint": "1.1.0", "docs": "1.0.0", "deploy": "1.0.0", "pinned": "1.0.0"})
	if !reflect.DeepEqual(f.record(t, "deploy"), deployBefore) {
		t.Error("deploy's lock entry changed")
	}
	if string(readFile(t, filepath.Join(f.proj, "skills.toml"))) != string(manifestBefore) {
		t.Error("skills.toml changed")
	}
	if strings.Contains(stdout, "deploy") {
		t.Errorf("output reports a skill outside the group:\n%s", stdout)
	}
}

// A relative and an absolute path to the same local repository are one
// group (spec 032 FR-002).
func TestUpdateRepo_PathSpellingsSelectTheSameGroup(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	rel, err := filepath.Rel(f.proj, f.alpha)
	if err != nil {
		t.Fatal(err)
	}
	abs, _, code := runGskill(t, f.proj, "--json", "update", "--list", "--all", "--repo", f.alpha)
	if code != 0 {
		t.Fatalf("absolute spelling exit %d", code)
	}
	relOut, _, code := runGskill(t, f.proj, "--json", "update", "--list", "--all", "--repo", rel)
	if code != 0 {
		t.Fatalf("relative spelling exit %d", code)
	}
	if abs != relOut {
		t.Errorf("spellings selected different reports:\n%s\n%s", abs, relOut)
	}
}

// Usage errors exit 2, explain themselves, and write nothing (spec 032
// FR-021, FR-024, FR-026, SC-004).
func TestUpdateRepo_UsageErrorsWriteNothing(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	before := f.files(t)
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"unknown repository", []string{"update", "--repo", "../nowhere"}, []string{"no tracked skill comes from", f.alpha, f.beta}},
		{"named skill elsewhere", []string{"update", "--repo", f.alpha, "deploy"}, []string{`skill "deploy" comes from ` + f.beta}},
		{"path below a repository", []string{"update", "--repo", filepath.Join(f.alpha, "lint")}, []string{"no tracked skill comes from"}},
		{"list with names", []string{"update", "--list", "lint"}, []string{"does not take skill names"}},
		{"all without list", []string{"update", "--all"}, []string{"--all requires --list"}},
	}
	for _, tt := range tests {
		_, stderr, code := runGskill(t, f.proj, append([]string{"--no-interactive"}, tt.args...)...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2: %s", tt.name, code, stderr)
		}
		for _, w := range tt.want {
			if !strings.Contains(stderr, w) {
				t.Errorf("%s: stderr missing %q:\n%s", tt.name, w, stderr)
			}
		}
	}
	if f.files(t) != before {
		t.Error("a usage error changed skills-lock.json or skills.toml")
	}
}

// A group in which nothing can move reports each skill and why, and exits 0
// (spec 032 FR-025).
func TestUpdateRepo_GroupWithNothingToUpdate(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	before := f.files(t)
	stdout, stderr, code := runGskill(t, f.proj, "--no-interactive", "update", "--repo", f.gamma)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"pinned", "pinned version", "gskill upgrade pinned"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if f.files(t) != before {
		t.Error("a no-op group changed the project files")
	}
}

func TestUpdateRepo_NamesNarrowTheGroup(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	if _, stderr, code := runGskill(t, f.proj, "--no-interactive", "update", "--repo", f.alpha, "lint"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	f.wantVersions(t, map[string]string{"lint": "1.1.0", "deploy": "1.0.0"})
}

func TestUpdateRepo_RepeatedReposCombine(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	if _, stderr, code := runGskill(t, f.proj, "--no-interactive", "update", "--repo", f.alpha, "--repo", f.beta); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	f.wantVersions(t, map[string]string{"lint": "1.1.0", "deploy": "1.1.0", "pinned": "1.0.0"})
}

// Without a selector the unattended update still moves every group (spec 032
// FR-033).
func TestUpdateRepo_NoSelectorStillUpdatesEverything(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	if _, stderr, code := runGskill(t, f.proj, "--no-interactive", "update"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	f.wantVersions(t, map[string]string{"lint": "1.1.0", "deploy": "1.1.0", "docs": "1.0.0", "pinned": "1.0.0"})
}

// --list --repo reports only the group, in one section, and changes nothing
// (spec 032 US3 scenario 9).
func TestUpdateRepo_ListScopesTheReport(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	before := f.files(t)
	stdout, stderr, code := runGskill(t, f.proj, "update", "--list", "--all", "--repo", f.alpha)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"lint", "docs", "pinned version"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "deploy") || strings.Count(stdout, "NAME ") != 1 {
		t.Errorf("report is not one section for the group:\n%s", stdout)
	}
	if f.files(t) != before {
		t.Error("--list changed the project files")
	}
}

func TestUpdateRepo_DryRunWritesNothing(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	before := f.files(t)
	stdout, stderr, code := runGskill(t, f.proj, "--dry-run", "--no-interactive", "update", "--repo", f.alpha)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "would update") || !strings.Contains(stdout, "Dry run: no changes made.") {
		t.Errorf("dry run does not report the would-be transitions:\n%s", stdout)
	}
	if f.files(t) != before {
		t.Error("dry run changed the project files")
	}
}

// JSON keeps one object and names each skill's repository (spec 032 FR-030).
func TestUpdateRepo_JSONNamesTheRepository(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	stdout, stderr, code := runGskill(t, f.proj, "--json", "update", "--repo", f.alpha)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var got struct {
		Skills []struct {
			Name       string `json:"name"`
			Repository string `json:"repository"`
			Local      bool   `json:"local"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("skills = %+v, want lint and docs", got.Skills)
	}
	for _, s := range got.Skills {
		if s.Repository != f.alpha || !s.Local {
			t.Errorf("%s: repository %q local %v, want %q true", s.Name, s.Repository, s.Local, f.alpha)
		}
	}
}

func TestUpdateRepo_OfflineChecksNothing(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	before := f.files(t)
	stdout, stderr, code := runGskill(t, f.proj, "--offline", "--no-interactive", "update", "--repo", f.beta)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "not checked") {
		t.Errorf("offline run does not say deploy was not checked:\n%s", stdout)
	}
	if f.files(t) != before {
		t.Error("offline run changed the project files")
	}
}

// The group is resolved from the declaration, so it still matches after its
// repository disappears; the failed lookup is a failure (spec 032 FR-032).
func TestUpdateRepo_FailedLookupInTheGroup(t *testing.T) {
	t.Parallel()

	f := newRepoFixture(t)
	if err := os.Rename(f.beta, f.beta+"-gone"); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runGskill(t, f.proj, "--no-interactive", "update", "--repo", f.beta)
	if code != 10 {
		t.Errorf("update exit %d, want 10: %s", code, stderr)
	}
	if !strings.Contains(stdout, "deploy") || !strings.Contains(stdout, "failed") {
		t.Errorf("stdout does not report deploy as failed:\n%s", stdout)
	}
	if _, stderr, code := runGskill(t, f.proj, "update", "--list", "--repo", f.beta); code != 5 {
		t.Errorf("--list exit %d, want 5: %s", code, stderr)
	}
}
