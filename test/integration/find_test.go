package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFind_SourceScopeJSON(t *testing.T) {
	t.Parallel()
	src := manySource(t, "kubernetes-ops", "writing")
	proj := newProject(t)

	stdout, stderr, code := runGskill(t, proj, "--json", "find", "kubernetes", "--source", src)
	if code != 0 {
		t.Fatalf("find exit %d: %s", code, stderr)
	}
	var hits []struct {
		ID       string `json:"id"`
		RepoPath string `json:"repo_path"`
	}
	if err := json.Unmarshal([]byte(stdout), &hits); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout)
	}
	if len(hits) != 1 || hits[0].ID != "kubernetes-ops" {
		t.Errorf("hits = %+v, want kubernetes-ops", hits)
	}
}

func TestFind_NoMatchExitsZero(t *testing.T) {
	t.Parallel()
	src := manySource(t, "writing")
	proj := newProject(t)

	stdout, _, code := runGskill(t, proj, "find", "nonexistent-xyz", "--source", src)
	if code != 0 {
		t.Errorf("find with no match should exit 0, got %d", code)
	}
	if stdout == "" {
		t.Error("expected a 'no matching skills' message")
	}
}

// Discovery scope and transport errors are usage errors that need no network
// (spec 035 FR-031).
func TestFind_ScopeAndTransportUsageErrors(t *testing.T) {
	t.Parallel()
	proj := newProject(t)

	_, stderr, code := runGskill(t, proj, "--no-interactive", "search", "x", "--owner", "git.example.com/acme")
	if code != 2 || !strings.Contains(stderr, "github.com, gitlab.com, and bitbucket.org") {
		t.Errorf("unsupported scope host: exit %d\n%s", code, stderr)
	}
	_, stderr, code = runGskill(t, proj, "--no-interactive", "search", "x", "--transport", "ssh")
	if code != 2 || !strings.Contains(stderr, "--transport applies only to repositories found with --owner") {
		t.Errorf("--transport without --owner: exit %d\n%s", code, stderr)
	}
}

// A hit from a nested GitLab project names its provider and full repository
// (spec 035 FR-030).
func TestFind_NestedGitLabHitNamesRepository(t *testing.T) { //nolint:paralleltest // sets GIT_CONFIG_*
	fixture := providerFixture(t)
	rewriteRemotes(t, fixture, "https://gitlab.com/acme/platform/skills.git")
	proj := newProject(t)

	stdout, stderr, code := runGskill(t, proj, "--no-interactive", "--json", "search", "lint",
		"--source", "https://gitlab.com/acme/platform/skills.git")
	if code != 0 {
		t.Fatalf("search: exit %d\n%s", code, stderr)
	}
	var hits []struct {
		ID         string `json:"id"`
		Source     string `json:"source"`
		Provider   string `json:"provider"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(stdout), &hits); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout)
	}
	if len(hits) != 1 || hits[0].Provider != "gitlab" || hits[0].Repository != "gitlab.com/acme/platform/skills" ||
		hits[0].Source != "https://gitlab.com/acme/platform/skills.git" {
		t.Errorf("hits = %+v, want lint from gitlab.com/acme/platform/skills", hits)
	}

	text, _, _ := runGskill(t, proj, "--no-interactive", "search", "lint", "--source", "https://gitlab.com/acme/platform/skills.git")
	if !strings.Contains(text, "gitlab.com/acme/platform/skills") {
		t.Errorf("text output does not name the repository:\n%s", text)
	}
}
