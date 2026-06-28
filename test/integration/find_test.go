package integration_test

import (
	"encoding/json"
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
