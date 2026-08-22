package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// declareOverride appends an override block for name to the project manifest.
func declareOverride(t *testing.T, proj, name, block string) {
	t.Helper()
	data, err := os.ReadFile(manifestPath(proj))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	out := string(data) + "\n[skills." + name + ".override]\n" + block
	if err := os.WriteFile(manifestPath(proj), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readLock(t *testing.T, proj string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(proj, "skills-lock.json")) //nolint:gosec // test reads its own temp project
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	return string(data)
}

// TestReconcile_DeclaredOverrideIsApplied proves the feature end to end through
// the CLI: declaring an override and running install materializes the
// customized content and records its identity.
func TestReconcile_DeclaredOverrideIsApplied(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
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

	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")) //nolint:gosec // test reads its own temp project
	if err != nil {
		t.Fatalf("read committed content: %v", err)
	}
	if !strings.Contains(string(content), "House rules") {
		t.Errorf("override not applied to committed content:\n%s", content)
	}
	if lock := readLock(t, proj); !strings.Contains(lock, "overrideDigest") {
		t.Errorf("override identity not recorded in the lock:\n%s", lock)
	}
}

// TestReconcile_UnchangedDeclarationIsIdempotent is FR-012: an entry whose
// declaration still matches the lock is used unchanged, so a second install
// rewrites nothing.
func TestReconcile_UnchangedDeclarationIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("first install: %s", stderr)
	}

	before := readLock(t, proj)
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("second install: %s", stderr)
	}
	if after := readLock(t, proj); after != before {
		t.Errorf("unchanged declaration rewrote the lock\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// TestReconcile_ChangedOverrideReResolves: editing the declaration changes the
// recorded identity, so install re-materializes rather than trusting the lock.
func TestReconcile_ChangedOverrideReResolves(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	rules := filepath.Join(proj, "gskill", "demo", "rules.md")
	if err := os.MkdirAll(filepath.Dir(rules), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declareOverride(t, proj, "demo", "append = [\"gskill/demo/rules.md\"]\n")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	first := readLock(t, proj)

	// Editing a referenced input changes the declaration's identity (FR-010).
	if err := os.WriteFile(rules, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("re-install: %s", stderr)
	}

	if second := readLock(t, proj); second == first {
		t.Error("editing a referenced override input must change the recorded identity")
	}
	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")) //nolint:gosec // test reads its own temp project
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "second") {
		t.Errorf("content not re-materialized after the override changed:\n%s", content)
	}
}
