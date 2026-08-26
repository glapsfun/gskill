package overrides_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/overrides"
)

// skillDir stages an "upstream extract" to transform.
func skillDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	return skillDir(t, files)
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestApply_Order pins FR-004: replace establishes the file that ships, patch
// adjusts that file, and layering goes last so appended content is never itself
// patched. The fixture is built so a wrong order produces a different result.
func TestApply_Order(t *testing.T) {
	t.Parallel()

	skill := skillDir(t, map[string]string{"SKILL.md": "upstream\n"})
	repo := repoWith(t, map[string]string{
		"g/SKILL.md": "replaced\n",
		// Applies only to the replaced content, proving replace ran first.
		"g/p.diff":  "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-replaced\n+patched\n",
		"g/tail.md": "appended\n",
	})

	err := overrides.Apply(skill, repo, overrides.Spec{
		Replace: map[string]string{"SKILL.md": "g/SKILL.md"},
		Patch:   []string{"g/p.diff"},
		Append:  []string{"g/tail.md"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := read(t, skill, "SKILL.md")
	if !strings.Contains(got, "patched") {
		t.Errorf("patch did not apply to the replaced content: %q", got)
	}
	if !strings.HasSuffix(got, "appended\n") {
		t.Errorf("layering must come last: %q", got)
	}
	if strings.Contains(got, "upstream") {
		t.Errorf("replace did not take effect: %q", got)
	}
}

// TestApply_DeclaredListOrder: patches apply in the order declared, so a later
// patch sees the earlier one's result.
func TestApply_DeclaredListOrder(t *testing.T) {
	t.Parallel()

	skill := skillDir(t, map[string]string{"SKILL.md": "a\n"})
	repo := repoWith(t, map[string]string{
		"g/1.diff": "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-a\n+b\n",
		"g/2.diff": "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-b\n+c\n",
	})

	if err := overrides.Apply(skill, repo, overrides.Spec{Patch: []string{"g/1.diff", "g/2.diff"}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := read(t, skill, "SKILL.md"); got != "c\n" {
		t.Errorf("sequential patches = %q, want %q", got, "c\n")
	}
}

// TestApply_Deterministic is SC-002: the same inputs always produce the same
// bytes, which is what makes the committed result reproducible.
func TestApply_Deterministic(t *testing.T) {
	t.Parallel()

	repo := repoWith(t, map[string]string{
		"g/x.md": "x\n", "g/y.md": "y\n",
		"g/r.md": "replaced\n",
	})
	spec := overrides.Spec{
		Replace: map[string]string{"OTHER.md": "g/r.md"},
		Prepend: []string{"g/x.md"},
		Append:  []string{"g/y.md"},
	}

	var results []string
	for range 20 {
		skill := skillDir(t, map[string]string{"SKILL.md": "body\n", "OTHER.md": "other\n"})
		if err := overrides.Apply(skill, repo, spec); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		results = append(results, read(t, skill, "SKILL.md")+"|"+read(t, skill, "OTHER.md"))
	}
	for i, r := range results {
		if r != results[0] {
			t.Fatalf("run %d differs:\n%q\nvs\n%q", i, r, results[0])
		}
	}
	if !strings.HasPrefix(results[0], "x\n") || !strings.Contains(results[0], "body") {
		t.Errorf("prepend/append not applied as expected: %q", results[0])
	}
}

// TestApply_Empty leaves the tree untouched, so a non-overridden skill keeps
// the hash it had (FR-008).
func TestApply_Empty(t *testing.T) {
	t.Parallel()

	skill := skillDir(t, map[string]string{"SKILL.md": "unchanged\n"})
	if err := overrides.Apply(skill, t.TempDir(), overrides.Spec{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := read(t, skill, "SKILL.md"); got != "unchanged\n" {
		t.Errorf("empty spec modified content: %q", got)
	}
}
