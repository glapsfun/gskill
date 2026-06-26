package integration_test

import (
	"strings"
	"testing"
)

func TestAddInvalidFrontmatter_Rejected(t *testing.T) {
	t.Parallel()

	// SKILL.md missing the required name field.
	repo := gitRepo(t, "---\ndescription: no name here\n---\n# body\n", "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}

	_, stderr, code := runGskill(t, proj, "add", repo)
	if code == 0 {
		t.Fatal("add succeeded with invalid frontmatter, want rejection")
	}
	if !strings.Contains(strings.ToLower(stderr), "frontmatter") &&
		!strings.Contains(strings.ToLower(stderr), "name") {
		t.Errorf("error should explain the invalid frontmatter: %q", stderr)
	}
}
