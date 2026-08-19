package overrides_test

import (
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/overrides"
)

// TestApply_PatchStrict is FR-011: application is strict, with zero fuzz, so
// "does not apply cleanly" is unambiguous and always an error.
func TestApply_PatchStrict(t *testing.T) {
	t.Parallel()

	skill := skillDir(t, map[string]string{"SKILL.md": "context has drifted\n"})
	repo := repoWith(t, map[string]string{
		"g/p.diff": "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-original line\n+new line\n",
	})

	err := overrides.Apply(skill, repo, overrides.Spec{Patch: []string{"g/p.diff"}})
	if err == nil {
		t.Fatal("a patch whose context no longer matches must fail")
	}
	if !errors.Is(err, errs.ErrIntegrity) {
		t.Errorf("want exit code 6 (ErrIntegrity), got %v", err)
	}
	// FR-011: nothing partially applied, prior content untouched.
	if got := read(t, skill, "SKILL.md"); got != "context has drifted\n" {
		t.Errorf("failed patch modified content: %q", got)
	}
}

// TestApply_PatchAtomicAcrossFiles: when a later patch fails, earlier ones in
// the same run must not survive — the operation is all-or-nothing.
func TestApply_PatchAtomicAcrossFiles(t *testing.T) {
	t.Parallel()

	skill := skillDir(t, map[string]string{"SKILL.md": "a\n"})
	repo := repoWith(t, map[string]string{
		"g/ok.diff":  "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-a\n+b\n",
		"g/bad.diff": "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-nope\n+z\n",
	})

	err := overrides.Apply(skill, repo, overrides.Spec{Patch: []string{"g/ok.diff", "g/bad.diff"}})
	if err == nil {
		t.Fatal("second patch must fail")
	}
	if got := read(t, skill, "SKILL.md"); got != "a\n" {
		t.Errorf("partial application survived: %q, want the original", got)
	}
}

// TestApply_PatchMissingTarget names the file rather than failing obscurely.
func TestApply_PatchMissingTarget(t *testing.T) {
	t.Parallel()

	skill := skillDir(t, map[string]string{"SKILL.md": "x\n"})
	repo := repoWith(t, map[string]string{
		"g/p.diff": "--- a/NOPE.md\n+++ b/NOPE.md\n@@ -1 +1 @@\n-x\n+y\n",
	})

	err := overrides.Apply(skill, repo, overrides.Spec{Patch: []string{"g/p.diff"}})
	if err == nil {
		t.Fatal("patch against a missing file must fail")
	}
}
