package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dupSource builds a source with two distinct folders that normalize to the
// same identity "dup".
func dupSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sp := range []string{"skills/a/dup", "skills/b/dup"} {
		dir := filepath.Join(root, filepath.FromSlash(sp))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(validSkill("dup")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// invalidSource builds a source with one valid and one invalid (no description) skill.
func invalidSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	okDir := filepath.Join(root, "skills", "ok")
	if err := os.MkdirAll(okDir, 0o750); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(okDir, "SKILL.md"), []byte(validSkill("ok")), 0o600)
	badDir := filepath.Join(root, "skills", "broken")
	if err := os.MkdirAll(badDir, 0o750); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte("---\nname: broken\n---\n# broken\n"), 0o600)
	return root
}

func TestAddSafety_DuplicateBareNameFails(t *testing.T) {
	t.Parallel()
	src := dupSource(t)
	proj := newProject(t)
	mustInit(t, proj)

	_, stderr, code := runGskill(t, proj, "--no-interactive", "add", src, "--skill", "dup")
	if code != 2 {
		t.Errorf("exit = %d, want 2 (ambiguous duplicate)", code)
	}
	if !strings.Contains(stderr, "skills/a/dup") || !strings.Contains(stderr, "skills/b/dup") {
		t.Errorf("error should list both conflicting paths: %q", stderr)
	}
	if got := installedDirs(t, proj); len(got) != 0 {
		t.Errorf("nothing should be installed, got %v", got)
	}
}

func TestAddSafety_DuplicatePathQualifiedInstalls(t *testing.T) {
	t.Parallel()
	src := dupSource(t)
	proj := newProject(t)
	mustInit(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", src, "--skill", "dup@skills/a/dup"); code != 0 {
		t.Fatalf("path-qualified add exit %d: %s", code, stderr)
	}
	if got := installedDirs(t, proj); len(got) != 1 || got[0] != "dup" {
		t.Errorf("installed = %v, want [dup]", got)
	}
}

func TestAddSafety_WildcardRefusesOnDuplicate(t *testing.T) {
	t.Parallel()
	src := dupSource(t)
	proj := newProject(t)
	mustInit(t, proj)

	_, _, code := runGskill(t, proj, "--no-interactive", "add", src, "--skill", "*")
	if code != 2 {
		t.Errorf("exit = %d, want 2 (wildcard refuses on duplicate)", code)
	}
	if got := installedDirs(t, proj); len(got) != 0 {
		t.Errorf("nothing should be installed, got %v", got)
	}
}

func TestAddSafety_ExplicitInvalidFails(t *testing.T) {
	t.Parallel()
	src := invalidSource(t)
	proj := newProject(t)
	mustInit(t, proj)

	_, _, code := runGskill(t, proj, "add", src, "--skill", "broken")
	if code != 3 {
		t.Errorf("exit = %d, want 3 (explicit invalid)", code)
	}
}

func TestAddSafety_WildcardInstallsValidSkipsInvalid(t *testing.T) {
	t.Parallel()
	src := invalidSource(t)
	proj := newProject(t)
	mustInit(t, proj)

	if _, stderr, code := runGskill(t, proj, "add", src, "--skill", "*"); code != 0 {
		t.Fatalf("wildcard add exit %d: %s", code, stderr)
	}
	got := installedDirs(t, proj)
	if len(got) != 1 || got[0] != "ok" {
		t.Errorf("installed = %v, want [ok] (broken skipped)", got)
	}
}
