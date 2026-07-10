package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdate_CaretFloatsToNewerTag is the finding-1 regression: a bare add
// records a caret range as the tracking intent, so a newly published
// compatible tag is reported by `outdated` and installed by `update` (an
// exact pin would have frozen it).
func TestUpdate_CaretFloatsToNewerTag(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"requestedVersion": "^1.2.0"`) {
		t.Fatalf("bare add did not record caret range in the lock:\n%s", lock)
	}

	// Publish a newer compatible tag.
	gitRun(t, repo, "tag", "v1.3.0")

	stdout, stderr, code := runGskill(t, proj, "outdated", "--json")
	if code != 0 {
		t.Fatalf("outdated: %s", stderr)
	}
	if !strings.Contains(stdout, `"available": true`) || !strings.Contains(stdout, `"latest": "1.3.0"`) {
		t.Errorf("outdated did not report 1.3.0 available:\n%s", stdout)
	}

	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	lock = string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.3.0"`) {
		t.Errorf("update did not bump the lock to 1.3.0:\n%s", lock)
	}
	// The tracking intent is unchanged (still the caret).
	if !strings.Contains(lock, `"requestedVersion": "^1.2.0"`) {
		t.Errorf("update changed the tracking intent:\n%s", lock)
	}
}
