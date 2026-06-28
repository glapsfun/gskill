package discovery_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/discovery"
)

func ids(r discovery.Result) []string {
	out := make([]string, 0, len(r.Skills))
	for _, s := range r.Skills {
		out = append(out, s.ID)
	}
	return out
}

func find(r discovery.Result, id string) (discovery.DiscoveredSkill, bool) {
	for _, s := range r.Skills {
		if s.ID == id {
			return s, true
		}
	}
	return discovery.DiscoveredSkill{}, false
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDiscoverAll_SingleRootSkill(t *testing.T) {
	t.Parallel()

	r, err := discovery.DiscoverAll("testdata/one-root-skill", discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(r.Skills) != 1 {
		t.Fatalf("got %d skills, want 1: %v", len(r.Skills), ids(r))
	}
	s := r.Skills[0]
	if s.RepoPath != "" {
		t.Errorf("RepoPath = %q, want empty (root)", s.RepoPath)
	}
	if s.ID != "one-root-skill" {
		t.Errorf("ID = %q, want one-root-skill (from source base)", s.ID)
	}
	if !s.Valid {
		t.Errorf("root skill should be valid: %+v", s.Problems)
	}
}

func TestDiscoverAll_RootIDOverride(t *testing.T) {
	t.Parallel()

	r, err := discovery.DiscoverAll("testdata/one-root-skill", discovery.Options{RootID: "my-repo"})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if s := r.Skills[0]; s.ID != "my-repo" {
		t.Errorf("ID = %q, want my-repo (RootID override)", s.ID)
	}
}

func TestDiscoverAll_NestedAndDeep(t *testing.T) {
	t.Parallel()

	cases := []struct {
		root, id, path string
	}{
		{"testdata/one-nested-skill", "foo", "skills/foo"},
		{"testdata/one-deep-skill", "c", "a/b/c"},
	}
	for _, c := range cases {
		r, err := discovery.DiscoverAll(c.root, discovery.Options{})
		if err != nil {
			t.Fatalf("DiscoverAll(%s): %v", c.root, err)
		}
		if len(r.Skills) != 1 {
			t.Fatalf("%s: got %d skills, want 1", c.root, len(r.Skills))
		}
		s := r.Skills[0]
		if s.ID != c.id || s.RepoPath != c.path {
			t.Errorf("%s: got id=%q path=%q, want id=%q path=%q", c.root, s.ID, s.RepoPath, c.id, c.path)
		}
	}
}

func TestDiscoverAll_ManySorted(t *testing.T) {
	t.Parallel()

	r, err := discovery.DiscoverAll("testdata/many-skills", discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	want := []string{"code-review", "convex-best-practices", "kubernetes-ops", "writing"}
	if got := ids(r); !eq(got, want) {
		t.Errorf("ids = %v, want %v (sorted by repo-path,id)", got, want)
	}
}

func TestDiscoverAll_Categorized(t *testing.T) {
	t.Parallel()

	r, err := discovery.DiscoverAll("testdata/categorized", discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if got, want := ids(r), []string{"api-design", "frontend-design"}; !eq(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestDiscoverAll_Duplicates(t *testing.T) {
	t.Parallel()

	r, err := discovery.DiscoverAll("testdata/duplicate-id", discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(r.Duplicates) != 1 {
		t.Fatalf("got %d duplicate conflicts, want 1", len(r.Duplicates))
	}
	d := r.Duplicates[0]
	if d.ID != "shared" {
		t.Errorf("duplicate ID = %q, want shared", d.ID)
	}
	if want := []string{"skills/a/shared", "skills/b/shared"}; !eq(d.Paths, want) {
		t.Errorf("duplicate paths = %v, want %v", d.Paths, want)
	}
}

func TestDiscoverAll_InvalidNotAborting(t *testing.T) {
	t.Parallel()

	r, err := discovery.DiscoverAll("testdata/with-problems", discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	broken, ok := find(r, "broken")
	if !ok {
		t.Fatal("broken skill not discovered")
	}
	if broken.Valid {
		t.Error("broken skill (missing description) should be invalid")
	}
	if len(broken.Problems) == 0 {
		t.Error("broken skill should carry a diagnostic")
	}
	okSkill, ok := find(r, "ok")
	if !ok || !okSkill.Valid {
		t.Error("valid skill should still be discovered and valid")
	}
}

func TestDiscoverAll_MismatchWarning(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "skills", "real-folder")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	// Frontmatter name differs from the folder slug → warning, still valid.
	content := "---\nname: other-name\ndescription: mismatch test\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := discovery.DiscoverAll(root, discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	s := r.Skills[0]
	if s.ID != "real-folder" {
		t.Errorf("ID = %q, want real-folder (folder wins)", s.ID)
	}
	if !s.Valid {
		t.Error("mismatch must not clear validity")
	}
	var warned bool
	for _, p := range s.Problems {
		if p.Severity == discovery.SeverityWarning {
			warned = true
		}
	}
	if !warned {
		t.Error("expected a mismatch warning diagnostic")
	}
}

func TestDiscoverAll_PrunesIgnoredDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// A real skill plus a SKILL.md buried in node_modules and .git.
	realDir := filepath.Join(root, "skills", "real")
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: real\ndescription: real one\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(realDir, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ignored := range []string{"node_modules/pkg", ".git", "vendor/x"} {
		d := filepath.Join(root, ignored)
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(skill), 0o600)
	}
	r, err := discovery.DiscoverAll(root, discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if got := ids(r); !eq(got, []string{"real"}) {
		t.Errorf("ids = %v, want [real] (ignored dirs pruned)", got)
	}
}

func TestDiscoverAll_MaxDepth(t *testing.T) {
	t.Parallel()

	// categorized skills live at depth 3 (skills/<cat>/<skill>).
	r, err := discovery.DiscoverAll("testdata/categorized", discovery.Options{MaxDepth: 2})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(r.Skills) != 0 {
		t.Errorf("MaxDepth 2 should exclude depth-3 skills, got %v", ids(r))
	}
}

func TestDiscoverAll_IncludeExclude(t *testing.T) {
	t.Parallel()

	inc, err := discovery.DiscoverAll("testdata/many-skills", discovery.Options{Include: []string{"skills/code-review"}})
	if err != nil {
		t.Fatalf("DiscoverAll include: %v", err)
	}
	if got := ids(inc); !eq(got, []string{"code-review"}) {
		t.Errorf("include ids = %v, want [code-review]", got)
	}

	exc, err := discovery.DiscoverAll("testdata/many-skills", discovery.Options{Exclude: []string{"skills/writing"}})
	if err != nil {
		t.Fatalf("DiscoverAll exclude: %v", err)
	}
	for _, id := range ids(exc) {
		if id == "writing" {
			t.Error("writing should have been excluded")
		}
	}
}

func TestDiscoverAll_Deterministic(t *testing.T) {
	t.Parallel()

	a, _ := discovery.DiscoverAll("testdata/many-skills", discovery.Options{})
	b, _ := discovery.DiscoverAll("testdata/many-skills", discovery.Options{})
	if !eq(ids(a), ids(b)) {
		t.Error("discovery must be deterministic across runs")
	}
}

func TestDiscoverAll_AgentScopedLayouts(t *testing.T) {
	t.Parallel()

	r, err := discovery.DiscoverAll("testdata/agent-scoped", discovery.Options{})
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if got, want := ids(r), []string{"x", "y", "z"}; !eq(got, want) {
		t.Errorf("ids = %v, want %v (agent-scoped dirs are discovered)", got, want)
	}
}
