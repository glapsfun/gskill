package docsite

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const landingMDX = "---\ntitle: GSKILL\ntemplate: splash\n---\n\nWelcome.\n"

func siteTree() map[string]string {
	files := baseTree()
	files["website/src/landing/index.mdx"] = landingMDX
	return files
}

func TestBuild(t *testing.T) {
	t.Parallel()

	root := newTree(t, siteTree())
	plan, err := Build(DefaultOptions(root))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := make([]string, 0, len(plan.Files))
	for rel := range plan.Files {
		got = append(got, rel)
	}
	sort.Strings(got)
	want := []string{
		"website/src/content/docs/contributing/c.md",
		"website/src/content/docs/explanation/e.md",
		"website/src/content/docs/how-to/a.md",
		"website/src/content/docs/index.mdx",
		"website/src/content/docs/overview.md",
		"website/src/content/docs/reference/r.md",
		"website/src/generated/sidebar.json",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("planned files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if string(plan.Files["website/src/content/docs/index.mdx"]) != landingMDX {
		t.Error("landing page was not copied verbatim")
	}
	overview := string(plan.Files["website/src/content/docs/overview.md"])
	if !strings.Contains(overview, "title: \"GSKILL docs\"") {
		t.Errorf("overview frontmatter missing title:\n%s", overview)
	}
	contributing := string(plan.Files["website/src/content/docs/contributing/c.md"])
	if !strings.Contains(contributing, "[license]("+BlobBase+"/LICENSE)") {
		t.Errorf("repository link not rewritten:\n%s", contributing)
	}
	if _, err := os.Stat(filepath.Join(root, "website", "src", "content")); !os.IsNotExist(err) {
		t.Errorf("Build wrote to disk (stat err = %v)", err)
	}
}

func TestBuild_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "coverage failure",
			files: map[string]string{"docs/how-to/b.md": "# B\n\nB.\n"},
			want:  "docs/how-to/b.md is in neither docs/nav.yaml nor its exclude list",
		},
		{
			name:  "transform failure",
			files: map[string]string{"docs/how-to/a.md": "No title here.\n"},
			want:  "how-to/a.md: no # title",
		},
		{
			name:  "broken link",
			files: map[string]string{"docs/explanation/e.md": "# E\n\nSee [gone](gone.md).\n"},
			want:  "explanation/e.md: link target gone.md does not exist",
		},
		{
			name:  "invalid manifest",
			files: map[string]string{"docs/nav.yaml": strings.Replace(validNav, "base: /gskill", "base: /x", 1)},
			want:  "docs/nav.yaml: base must be /gskill",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := siteTree()
			for rel, content := range tt.files {
				files[rel] = content
			}
			_, err := Build(DefaultOptions(newTree(t, files)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Build error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestBuild_WithoutLanding(t *testing.T) {
	t.Parallel()

	plan, err := Build(DefaultOptions(newTree(t, baseTree())))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := plan.Files["website/src/content/docs/index.mdx"]; ok {
		t.Error("planned a landing page that does not exist")
	}
}

func TestPlan_Write(t *testing.T) {
	t.Parallel()

	root := newTree(t, siteTree())
	stale := filepath.Join(root, "website", "src", "content", "docs", "old", "gone.md")
	writeTree(t, root, map[string]string{"website/src/content/docs/old/gone.md": "stale"})

	plan, err := Build(DefaultOptions(root))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	written, err := plan.Write(root)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(written) != len(plan.Files) {
		t.Errorf("Write reported %d files, plan has %d", len(written), len(plan.Files))
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale page survived Write (stat err = %v)", err)
	}
	for rel, want := range plan.Files {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // test reads its own temp tree
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s on disk differs from the plan", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "README.md")); err != nil {
		t.Errorf("Write touched docs/: %v", err)
	}
}

func TestPlan_Check_IgnoresUntrackedOutputs(t *testing.T) {
	t.Parallel()

	root := newTree(t, siteTree())
	plan, err := Build(DefaultOptions(root))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	diff, err := plan.Check(root)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(diff) != 0 {
		t.Errorf("Check reported gitignored outputs as drift: %v", diff)
	}
}

func TestFindRepoRoot(t *testing.T) {
	t.Parallel()

	root := newTree(t, map[string]string{"go.mod": "module x\n", "a/b/c.txt": "x"})
	got, err := FindRepoRoot(filepath.Join(root, "a", "b"))
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	if got != root {
		t.Errorf("FindRepoRoot = %q, want %q", got, root)
	}
	if _, err := FindRepoRoot(t.TempDir()); err == nil {
		t.Error("FindRepoRoot found a go.mod above an empty temp dir")
	}
}

// installSiteTree is siteTree plus an installation page listed in Start here
// and a README whose install region is stale.
func installSiteTree() map[string]string {
	files := siteTree()
	files["docs/nav.yaml"] = strings.Replace(validNav, "        label: Overview\n", "        label: Overview\n      - installation.md\n", 1)
	files["docs/installation.md"] = installDoc(installRegion)
	files["README.md"] = "# GSKILL\n\n" + InstallStart + "\nstale\n" + InstallEnd + "\n"
	return files
}

func TestBuild_InstallPage(t *testing.T) {
	t.Parallel()

	root := newTree(t, installSiteTree())
	plan, err := Build(DefaultOptions(root))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, rel := range []string{
		"website/src/content/docs/installation.mdx",
		"website/src/generated/install-methods.json",
		"README.md",
	} {
		if _, ok := plan.Files[rel]; !ok {
			t.Errorf("plan is missing %s", rel)
		}
	}
	if _, ok := plan.Files["website/src/content/docs/installation.md"]; ok {
		t.Error("installation page planned as .md as well as .mdx")
	}

	diff, err := plan.Check(root)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if strings.Join(diff, ",") != "README.md (install region differs)" {
		t.Errorf("Check before Write = %v, want README.md (install region differs)", diff)
	}
	if _, err := plan.Write(root); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if diff, err := plan.Check(root); err != nil || len(diff) != 0 {
		t.Errorf("Check after Write = %v, %v; want no drift", diff, err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md")) //nolint:gosec // test reads its own temp tree
	if err != nil {
		t.Fatal(err)
	}
	if region, err := InstallRegion(string(readme)); err != nil || region != installRegion {
		t.Errorf("README region after Write = %q, %v", region, err)
	}
}

func TestBuild_InstallPageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, rel, content, want string
	}{
		{"readme without markers", "README.md", "# GSKILL\n", "README.md: no install-methods region"},
		{"invalid install region", "docs/installation.md", installDoc(strings.Replace(installRegion, "### Go\n", "### Golang\n", 1)), "installation.md: install region: methods must be exactly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := installSiteTree()
			files[tt.rel] = tt.content
			_, err := Build(DefaultOptions(newTree(t, files)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Build error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestBuild_CopiesImages(t *testing.T) {
	t.Parallel()

	files := siteTree()
	files["docs/img/d.svg"] = "<svg/>"
	files["docs/design/fig.png"] = "png"
	files["docs/explanation/e.md"] = "# Explain E\n\nWhy E works.\n\n![diagram](../img/d.svg)\n"
	plan, err := Build(DefaultOptions(newTree(t, files)))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := string(plan.Files["website/public/docs-assets/img/d.svg"]); got != "<svg/>" {
		t.Errorf("image not copied (got %q)", got)
	}
	if _, ok := plan.Files["website/public/docs-assets/design/fig.png"]; ok {
		t.Error("image under an excluded directory was copied")
	}
	if !strings.Contains(string(plan.Files["website/src/content/docs/explanation/e.md"]), "](/gskill/docs-assets/img/d.svg)") {
		t.Error("image link not rewritten to docs-assets")
	}
}

// TestBuild_ExcludedPagesAreNotPublished guards FR-025: an excluded page gets
// no output file, so it is never indexed by search, and links to it go to
// GitHub instead of a site URL that would 404.
func TestBuild_ExcludedPagesAreNotPublished(t *testing.T) {
	t.Parallel()

	files := siteTree()
	files["docs/explanation/e.md"] = "# Explain E\n\nWhy E works. See the [design notes](../design/x.md).\n"
	plan, err := Build(DefaultOptions(newTree(t, files)))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for rel := range plan.Files {
		if strings.Contains(rel, "design") {
			t.Errorf("excluded page published as %s", rel)
		}
	}
	page := string(plan.Files["website/src/content/docs/explanation/e.md"])
	if !strings.Contains(page, "[design notes]("+BlobBase+"/docs/design/x.md)") {
		t.Errorf("link to an excluded page not sent to GitHub:\n%s", page)
	}
}
