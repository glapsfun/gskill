package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdate_ReAppliesOverride is FR-018: `update` re-applies the declared
// override on top of the newer upstream. Without this an update would silently
// discard a user's customization while reporting success.
func TestUpdate_ReAppliesOverride(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	rules := filepath.Join(proj, "gskill", "demo", "rules.md")
	if err := os.MkdirAll(filepath.Dir(rules), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules, []byte("## House rules\nCite file:line.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declareOverride(t, proj, "demo", "append = [\"gskill/demo/rules.md\"]\n")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}

	publishNewVersion(t, repo, "demo", "v1.1.0")
	if _, stderr, code := runGskill(t, proj, "update", "demo"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}

	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")) //nolint:gosec // test reads its own temp project
	if err != nil {
		t.Fatalf("read committed content: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "House rules") {
		t.Errorf("update discarded the declared override:\n%s", got)
	}
	if !strings.Contains(got, "v1.1.0") {
		t.Errorf("update did not advance to the newer upstream:\n%s", got)
	}
}

// TestUpdate_AdvancesHashesTogether: base, digest, and content hashes describe
// one consistent state, so a half-advanced entry can never be recorded.
func TestUpdate_AdvancesHashesTogether(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	rules := filepath.Join(proj, "gskill", "demo", "rules.md")
	if err := os.MkdirAll(filepath.Dir(rules), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules, []byte("rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declareOverride(t, proj, "demo", "append = [\"gskill/demo/rules.md\"]\n")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	before := readLock(t, proj)

	publishNewVersion(t, repo, "demo", "v1.1.0")
	if _, stderr, code := runGskill(t, proj, "update", "demo"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}

	after := readLock(t, proj)
	if after == before {
		t.Fatal("update recorded no change")
	}
	for _, key := range []string{"baseHash", "overrideDigest", "contentHash"} {
		if !strings.Contains(after, key) {
			t.Errorf("lock lost %s after update:\n%s", key, after)
		}
	}
	// The declaration is unchanged, so its digest must survive the update.
	if !strings.Contains(after, "overrideDigest") {
		t.Error("override identity dropped by update")
	}
}
