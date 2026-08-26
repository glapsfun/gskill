package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manifestPath(proj string) string { return filepath.Join(proj, "skills.toml") }

func readManifest(t *testing.T, proj string) string {
	t.Helper()
	data, err := os.ReadFile(manifestPath(proj))
	if err != nil {
		t.Fatalf("read skills.toml: %v", err)
	}
	return string(data)
}

// TestManifestLifecycle_AddWritesDeclaration is FR-019: `add` records the
// declaration in the same run that writes the lock entry, so the authored and
// generated halves never disagree after a successful add.
func TestManifestLifecycle_AddWritesDeclaration(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	got := readManifest(t, proj)
	if !strings.Contains(got, "[skills.demo]") {
		t.Errorf("manifest missing the added skill:\n%s", got)
	}
	if !strings.Contains(got, "source =") {
		t.Errorf("manifest entry has no source:\n%s", got)
	}
}

// TestManifestLifecycle_RemoveDropsDeclaration is FR-020: `remove` deletes the
// declaration alongside the lock entry, leaving no orphaned intent behind.
func TestManifestLifecycle_RemoveDropsDeclaration(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "remove", "demo", "--force"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}

	if _, err := os.Stat(manifestPath(proj)); os.IsNotExist(err) {
		return // an empty manifest may be removed entirely; nothing orphaned
	}
	if got := readManifest(t, proj); strings.Contains(got, "[skills.demo]") {
		t.Errorf("declaration survived removal:\n%s", got)
	}
}

// TestManifestLifecycle_AddPreservesUserEdits: the manifest is hand-authored
// and committed, so a later `add` must not destroy comments a user wrote.
func TestManifestLifecycle_AddPreservesUserEdits(t *testing.T) {
	t.Parallel()

	first := gitRepo(t, validSkill("demo"), "v1.0.0")
	second := gitRepo(t, validSkill("other"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", first); code != 0 {
		t.Fatalf("first add: %s", stderr)
	}

	// A user annotates the generated manifest by hand.
	annotated := "# Our team's skills — do not delete this note.\n" + readManifest(t, proj)
	if err := os.WriteFile(manifestPath(proj), []byte(annotated), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "add", second); code != 0 {
		t.Fatalf("second add: %s", stderr)
	}

	got := readManifest(t, proj)
	if !strings.Contains(got, "do not delete this note") {
		t.Errorf("add destroyed a user comment:\n%s", got)
	}
	if !strings.Contains(got, "[skills.other]") {
		t.Errorf("second skill not declared:\n%s", got)
	}
}

// TestManifestLifecycle_PreservesVersionConstraint: a skill added with a
// semver constraint must be declared with that constraint, not with the tag it
// happened to resolve to. Writing `ref = "v1.0.0"` would silently convert a
// tracking range into a hard pin the user never asked for, and `update` would
// then have nothing to advance.
func TestManifestLifecycle_PreservesVersionConstraint(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	got := readManifest(t, proj)
	if !strings.Contains(got, `version = "^1.0.0"`) {
		t.Errorf("manifest lost the version constraint:\n%s", got)
	}
	if strings.Contains(got, `ref = "v1.0.0"`) {
		t.Errorf("constraint was rewritten as a hard pin:\n%s", got)
	}
}
