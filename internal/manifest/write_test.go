package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/manifest"
)

const commented = `# My project's skills.
# Keep this comment.

[config]
jobs = 4 # inline comment

# code-review does the reviews
[skills.code-review]
source = "github:org/skills"
ref    = "v2.1"

[skills.code-review.override]
append = ["gskill/cr/rules.md"]

# docs-writer writes the docs
[skills.docs-writer]
source = "github:org/docs"
`

// TestUpsert_PreservesComments is the risk this task exists to retire: a naive
// marshal round-trip silently destroys user comments. Writing must be surgical.
func TestUpsert_PreservesComments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), manifest.FileName)
	if err := os.WriteFile(path, []byte(commented), 0o600); err != nil {
		t.Fatal(err)
	}

	err := manifest.Upsert(path, manifest.Skill{
		Name: "new-skill", Source: "github:org/new", Ref: "v1",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got := readFile(t, path)
	for _, want := range []string{
		"# My project's skills.",
		"# Keep this comment.",
		"jobs = 4 # inline comment",
		"# code-review does the reviews",
		"# docs-writer writes the docs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment or formatting lost: %q\n--- file ---\n%s", want, got)
		}
	}
	if !strings.Contains(got, "[skills.new-skill]") {
		t.Errorf("new skill not written:\n%s", got)
	}
}

// TestUpsert_ReplacesInPlace: updating a skill rewrites only its own block and
// leaves neighbouring blocks and their comments untouched (minimal diff).
func TestUpsert_ReplacesInPlace(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), manifest.FileName)
	if err := os.WriteFile(path, []byte(commented), 0o600); err != nil {
		t.Fatal(err)
	}

	err := manifest.Upsert(path, manifest.Skill{
		Name: "code-review", Source: "github:org/skills", Commit: "abc123",
		Override: &manifest.Override{Append: []string{"gskill/cr/rules.md"}},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got := readFile(t, path)
	if strings.Count(got, "[skills.code-review]") != 1 {
		t.Errorf("block duplicated instead of replaced:\n%s", got)
	}
	if !strings.Contains(got, `commit = "abc123"`) {
		t.Errorf("update not applied:\n%s", got)
	}
	if !strings.Contains(got, "# docs-writer writes the docs") {
		t.Errorf("neighbouring comment lost:\n%s", got)
	}
	// The stale override sub-block must not survive alongside the rewritten one.
	if strings.Count(got, "[skills.code-review.override]") != 1 {
		t.Errorf("override sub-block duplicated:\n%s", got)
	}

	m, err := manifest.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if m.Skills["code-review"].Commit != "abc123" {
		t.Errorf("reload lost the commit: %+v", m.Skills["code-review"])
	}
	if m.Skills["docs-writer"].Source != "github:org/docs" {
		t.Errorf("neighbour damaged: %+v", m.Skills["docs-writer"])
	}
}

// TestRemove_DropsBlockAndSubTables: removing a skill takes its override
// sub-table with it and leaves nothing orphaned behind.
func TestRemove_DropsBlockAndSubTables(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), manifest.FileName)
	if err := os.WriteFile(path, []byte(commented), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manifest.Remove(path, "code-review"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got := readFile(t, path)
	if strings.Contains(got, "[skills.code-review]") || strings.Contains(got, "[skills.code-review.override]") {
		t.Errorf("block or sub-table survived removal:\n%s", got)
	}
	if !strings.Contains(got, "[skills.docs-writer]") || !strings.Contains(got, "# Keep this comment.") {
		t.Errorf("removal damaged unrelated content:\n%s", got)
	}

	m, err := manifest.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, still := m.Skills["code-review"]; still {
		t.Error("removed skill still parses")
	}
}

// TestUpsert_CreatesFile: `add` in a project without a manifest creates one.
func TestUpsert_CreatesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), manifest.FileName)
	err := manifest.Upsert(path, manifest.Skill{
		Name: "a", Source: "github:o/r", Agents: []string{"claude"}, Mode: manifest.ModeSymlink,
		Override: &manifest.Override{
			Replace: map[string]string{"SKILL.md": "gskill/a/SKILL.md"},
			Patch:   []string{"gskill/a/1.diff", "gskill/a/2.diff"},
		},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	m, err := manifest.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := m.Skills["a"]
	if s.Source != "github:o/r" || s.Mode != manifest.ModeSymlink {
		t.Errorf("round-trip lost fields: %+v", s)
	}
	if p := s.Override.Patch; len(p) != 2 || p[0] != "gskill/a/1.diff" {
		t.Errorf("declared patch order lost: %v", p)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestUpsert_KeepsOwnDocCommentAndPosition is the case an earlier test missed
// by only re-adding a *different* skill: re-declaring a skill that already
// exists must keep the comment documenting it and leave it where it stands.
// Re-appending the block at EOF would silently delete that comment on every
// `add` — exactly the damage splicing as text exists to avoid.
func TestUpsert_KeepsOwnDocCommentAndPosition(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), manifest.FileName)
	original := `# The review skill; do not remove.
[skills.demo]
source = "github:org/skills"
ref    = "v1"

# docs stay put
[skills.docs]
source = "github:org/docs"
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	err := manifest.Upsert(path, manifest.Skill{
		Name: "demo", Source: "github:org/skills", Ref: "v2",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got := readFile(t, path)
	if !strings.Contains(got, "# The review skill; do not remove.") {
		t.Errorf("re-declaring a skill deleted its own comment:\n%s", got)
	}
	if !strings.Contains(got, `ref = "v2"`) {
		t.Errorf("update not applied:\n%s", got)
	}
	// Position: demo must still precede docs.
	if strings.Index(got, "[skills.demo]") > strings.Index(got, "[skills.docs]") {
		t.Errorf("block was moved to the end instead of rewritten in place:\n%s", got)
	}
	if strings.Count(got, "[skills.demo]") != 1 {
		t.Errorf("block duplicated:\n%s", got)
	}
}
