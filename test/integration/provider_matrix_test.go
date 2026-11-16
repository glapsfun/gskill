package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/registry"
	"github.com/glapsfun/gskill/internal/testutil"
)

// failingLister fails the test if anything asks a provider API for
// repositories: adding and installing a Git source must use Git alone
// (spec 035 FR-017).
type failingLister struct{ t *testing.T }

func (f failingLister) ListRepos(context.Context, registry.Scope) (registry.Listing, error) {
	f.t.Error("a provider API was contacted during a Git-only operation")
	return registry.Listing{}, nil
}

// noAPIApp builds an App with a private, empty gskill home and a lister that
// fails the test when called.
func noAPIApp(t *testing.T) *app.App {
	t.Helper()
	return app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: filepath.Join(t.TempDir(), "gskill-home"),
		Repos:      failingLister{t: t},
	})
}

// rewriteRemotes points each address prefix at fixture through Git's
// url.<base>.insteadOf, which gskill passes to git through GIT_CONFIG_*.
// Callers must not be parallel.
func rewriteRemotes(t *testing.T, fixture string, prefixes ...string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(len(prefixes)))
	for i, prefix := range prefixes {
		n := strconv.Itoa(i)
		t.Setenv("GIT_CONFIG_KEY_"+n, "url."+fixture+".insteadOf")
		t.Setenv("GIT_CONFIG_VALUE_"+n, prefix)
	}
}

// providerFixture is a repository with skill demo tagged v1.0.0, v1.1.0, and
// v2.0.0, plus a second skill at tools/lint for folder-qualified sources.
func providerFixture(t *testing.T) string {
	t.Helper()
	repo := testutil.InitSkillRepo(t, "demo", testutil.SkillBody("demo", "v1.0.0"), "v1.0.0")
	lint := filepath.Join(repo, "tools", "lint")
	if err := os.MkdirAll(lint, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lint, "SKILL.md"), []byte(validSkill("lint")), 0o600); err != nil {
		t.Fatal(err)
	}
	testutil.GitRun(t, repo, "add", ".")
	testutil.GitRun(t, repo, "commit", "--quiet", "-m", "add lint")
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v1.1.0"), "v1.1.0")
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v2.0.0"), "v2.0.0")
	return repo
}

// lockEntry is the part of a skills-lock.json entry these tests check.
type lockEntry struct {
	SourceType string `json:"sourceType"`
	Gskill     struct {
		SourceURL   string `json:"sourceUrl"`
		Version     string `json:"version"`
		Ref         string `json:"ref"`
		Commit      string `json:"commit"`
		ContentHash string `json:"contentHash"`
	} `json:"gskill"`
}

func lockEntryOf(t *testing.T, proj, name string) lockEntry {
	t.Helper()
	var lock struct {
		Skills map[string]lockEntry `json:"skills"`
	}
	if err := json.Unmarshal(readFile(t, filepath.Join(proj, "skills-lock.json")), &lock); err != nil {
		t.Fatal(err)
	}
	e, ok := lock.Skills[name]
	if !ok {
		t.Fatalf("skills-lock.json has no %q entry", name)
	}
	return e
}

func runOK(t *testing.T, a *app.App, proj string, args ...string) {
	t.Helper()
	if stdout, stderr, code := runGskillWithApp(t, a, proj, append([]string{"--no-interactive"}, args...)...); code != 0 {
		t.Fatalf("gskill %v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
}

// freshCopy returns a new project holding only proj's skills.toml and
// skills-lock.json, the state a teammate gets from version control.
func freshCopy(t *testing.T, proj string) string {
	t.Helper()
	dst := newProject(t)
	for _, f := range []string{"skills.toml", "skills-lock.json"} {
		if err := os.WriteFile(filepath.Join(dst, f), readFile(t, filepath.Join(proj, f)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// providerCase is one provider and address spelling of the same fixture.
type providerCase struct {
	name, source, cloneURL string
	// rewrite is the address prefix git sees, redirected to the fixture.
	rewrite string
}

var providerCases = []providerCase{
	{"github/https", "https://github.com/acme/skills.git", "https://github.com/acme/skills.git", "https://github.com/acme/skills.git"},
	{"github/ssh", "git@github.com:acme/skills.git", "git@github.com:acme/skills.git", "git@github.com:acme/skills.git"},
	{"gitlab/https", "https://gitlab.com/acme/skills.git", "https://gitlab.com/acme/skills.git", "https://gitlab.com/acme/skills.git"},
	{"gitlab/ssh", "git@gitlab.com:acme/skills.git", "git@gitlab.com:acme/skills.git", "git@gitlab.com:acme/skills.git"},
	{"gitlab/ssh-url-port", "ssh://git@gitlab.com:2222/acme/skills.git", "ssh://git@gitlab.com:2222/acme/skills.git", "ssh://git@gitlab.com:2222/acme/skills.git"},
	{"bitbucket/https", "https://bitbucket.org/acme/skills.git", "https://bitbucket.org/acme/skills.git", "https://bitbucket.org/acme/skills.git"},
	{"bitbucket/ssh", "git@bitbucket.org:acme/skills.git", "git@bitbucket.org:acme/skills.git", "git@bitbucket.org:acme/skills.git"},
}

// TestProviderMatrix runs the lifecycle on every provider and transport
// against a local stand-in (spec 035 SC-001, FR-001, FR-002, FR-011, FR-017).
func TestProviderMatrix(t *testing.T) { //nolint:paralleltest // subtests set GIT_CONFIG_*
	for _, tc := range providerCases { //nolint:paralleltest // subtests set GIT_CONFIG_*
		t.Run(tc.name, func(t *testing.T) {
			fixture := providerFixture(t)
			rewriteRemotes(t, fixture, tc.rewrite)
			a := noAPIApp(t)
			proj := newProject(t)

			runOK(t, a, proj, "add", tc.source, "--skill", "demo", "--version", "^1.0.0", "--agent", "claude")
			added := lockEntryOf(t, proj, "demo")
			if added.SourceType != "git" || added.Gskill.SourceURL != tc.cloneURL || added.Gskill.Version != "1.1.0" {
				t.Fatalf("add locked sourceType %q url %q version %q, want git %q 1.1.0",
					added.SourceType, added.Gskill.SourceURL, added.Gskill.Version, tc.cloneURL)
			}

			// A teammate's clean checkout on a fresh machine: the clone cache
			// is cold, so git really fetches over the recorded address.
			lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))
			teammate := freshCopy(t, proj)
			runOK(t, noAPIApp(t), teammate, "install", "--frozen-lockfile")
			if got := readFile(t, filepath.Join(teammate, "skills-lock.json")); !bytes.Equal(got, lockBefore) {
				t.Errorf("frozen install rewrote the lockfile:\n%s", got)
			}

			runOK(t, a, proj, "update")
			if v := lockEntryOf(t, proj, "demo").Gskill.Version; v != "1.1.0" {
				t.Errorf("update moved demo to %q within ^1.0.0, want 1.1.0", v)
			}
			runOK(t, a, proj, "--json", "upgrade", "demo", "--to", "2.0.0")
			upgraded := lockEntryOf(t, proj, "demo")
			if upgraded.Gskill.Version != "2.0.0" || upgraded.Gskill.SourceURL != tc.cloneURL {
				t.Errorf("upgrade locked %q at %q, want 2.0.0 at %q", upgraded.Gskill.SourceURL, upgraded.Gskill.Version, tc.cloneURL)
			}
			runOK(t, a, proj, "--json", "project", "verify")

			pinned := newProject(t)
			runOK(t, a, pinned, "add", tc.source, "--skill", "demo", "--ref", "v1.0.0", "--agent", "claude")
			if e := lockEntryOf(t, pinned, "demo"); e.Gskill.Ref != "v1.0.0" || e.Gskill.SourceURL != tc.cloneURL {
				t.Errorf("--ref add locked ref %q url %q", e.Gskill.Ref, e.Gskill.SourceURL)
			}
			commit := strings.TrimSpace(gitOutput(t, fixture, "rev-list", "-n", "1", "v1.0.0"))
			byCommit := newProject(t)
			runOK(t, a, byCommit, "add", tc.source, "--skill", "demo", "--commit", commit, "--agent", "claude")
			if e := lockEntryOf(t, byCommit, "demo"); e.Gskill.Commit != commit || e.Gskill.SourceURL != tc.cloneURL {
				t.Errorf("--commit add locked commit %q url %q, want %q", e.Gskill.Commit, e.Gskill.SourceURL, commit)
			}
		})
	}
}

// TestProviderMatrix_NestedGitLab resolves nested GitLab projects and
// folder-qualified sources, and refuses the ambiguous spelling before any
// git process runs (spec 035 US2, SC-002).
func TestProviderMatrix_NestedGitLab(t *testing.T) {
	fixture := providerFixture(t)
	rewriteRemotes(t, fixture,
		"https://gitlab.com/acme/platform/skills.git",
		"git@gitlab.com:acme/platform/skills.git")

	cases := []struct {
		source, skill, cloneURL string
	}{
		{"https://gitlab.com/acme/platform/skills.git", "demo", "https://gitlab.com/acme/platform/skills.git"},
		{"git@gitlab.com:acme/platform/skills.git", "demo", "git@gitlab.com:acme/platform/skills.git"},
		{"gitlab.com/acme/platform/skills//tools/lint", "lint", "https://gitlab.com/acme/platform/skills.git"},
		{"git@gitlab.com:acme/platform/skills.git//tools/lint", "lint", "git@gitlab.com:acme/platform/skills.git"},
	}
	for _, tc := range cases {
		proj := newProject(t)
		args := []string{"add", tc.source, "--ref", "v2.0.0", "--agent", "claude"}
		if tc.skill == "demo" {
			args = append(args, "--skill", "demo")
		}
		runOK(t, noAPIApp(t), proj, args...)
		if e := lockEntryOf(t, proj, tc.skill); e.Gskill.SourceURL != tc.cloneURL {
			t.Errorf("%s: locked url %q, want %q", tc.source, e.Gskill.SourceURL, tc.cloneURL)
		}
		if _, err := os.Stat(filepath.Join(proj, ".agents", "skills", tc.skill, "SKILL.md")); err != nil {
			t.Errorf("%s: %s not installed: %v", tc.source, tc.skill, err)
		}
	}

	// An SSH command that fails loudly proves no git process reached the
	// network for the ambiguous spelling.
	t.Setenv("GIT_SSH_COMMAND", "false")
	proj := newProject(t)
	_, stderr, code := runGskillWithApp(t, noAPIApp(t), proj,
		"--no-interactive", "add", "git@gitlab.com:acme/platform/skills", "--agent", "claude")
	if code != 2 {
		t.Fatalf("ambiguous source exit %d, want 2\n%s", code, stderr)
	}
	for _, want := range []string{"git@gitlab.com:acme/platform/skills.git", "git@gitlab.com:acme/platform//skills"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks the spelling %q:\n%s", want, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, "skills-lock.json")); err == nil {
		t.Error("the refused source wrote a lockfile")
	}
}

// gitOutput runs git in dir and returns its stdout.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = testutil.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
