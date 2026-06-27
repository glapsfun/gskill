package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAddBranch_EmitsMutableWarning(t *testing.T) {
	t.Parallel()

	// A repo with no tags forces a mutable branch resolution.
	repo := gitRepo(t, validSkill("demo"))
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}

	stdout, stderr, code := runGskill(t, proj, "add", repo, "--ref", "main", "--json")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	// Warning surfaces on stderr.
	if !strings.Contains(strings.ToLower(stderr), "mutable") {
		t.Errorf("stderr should warn about a mutable ref: %q", stderr)
	}

	// Warning also appears in the --json warnings field (SC-008).
	var result struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("add json: %v\n%s", err, stdout)
	}
	if len(result.Warnings) == 0 {
		t.Errorf("expected a warning in the --json output, got none: %s", stdout)
	}
}
