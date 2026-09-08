package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// retiredInvocationRE matches a retired command spelling written out as an
// invocation — in a hint, a diagnostic, a doc comment, or documentation prose.
// The `project` prefix is excluded so the canonical forms do not match.
var retiredInvocationRE = regexp.MustCompile(
	`gskill (?:sync|repair|verify|check|diff|status|init|source|unlink|store|cache path|config path|project diff)\b`)

// historicalMentionRE marks a line that names a retired spelling as history
// rather than as advice to run it.
var historicalMentionRE = regexp.MustCompile(`(?i)\b(retired|removed|former|no longer|used to)\b`)

// guardExempt are the files allowed to name a retired spelling: the tests that
// exist precisely to assert those spellings are rejected, this guard itself,
// and the historical design record.
var guardExempt = map[string]bool{
	"retired_spellings_test.go":     true,
	"flat_aliases_removed_test.go":  true,
	"path_commands_removed_test.go": true,
	"project_diff_removed_test.go":  true,
	"init_removed_test.go":          true,
	"source_removed_test.go":        true,
	"status_removed_test.go":        true,
	"store_removed_test.go":         true,
	"unlink_removed_test.go":        true,
	"documentation-redesign.md":     true,
}

// TestNoRetiredSpellingsInShippedText is the guard the *_removed_test.go files
// do not provide: they lock the grammar, but a retired spelling can still hide
// in a string literal the tool prints. Spec 025 shipped three error hints
// telling users to run `gskill repair` after that command started exiting 2 —
// advice that was dead at exactly the moment a user needed it. This walks the
// Go sources and the documentation so the next removal cannot repeat it.
func TestNoRetiredSpellingsInShippedText(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	// specs/ and every dot-directory (.docs/, .specify/, .git/) are planning
	// history, and docs/design/ is a historical record; all legitimately
	// describe the surface as it once was.
	skipDirs := map[string]bool{"bin": true, "specs": true, "node_modules": true, "testdata": true}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".md" {
			return nil
		}
		if guardExempt[d.Name()] {
			return nil
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // test-controlled repo walk
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(content), "\n") {
			if historicalMentionRE.MatchString(line) {
				// Migration guidance ("the former X was retired; use Y") names
				// a retired spelling on purpose. It tells a reader what NOT to
				// run, which is the opposite of the bug this guards against.
				continue
			}
			if hit := retiredInvocationRE.FindString(line); hit != "" {
				t.Errorf("%s:%d names the retired invocation %q:\n\t%s",
					rel, i+1, hit, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}
