package docsite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNav(t *testing.T) {
	t.Parallel()

	nav, err := ParseNav([]byte(validNav), "docs/nav.yaml")
	if err != nil {
		t.Fatalf("ParseNav(valid): %v", err)
	}
	if nav.Base != "/gskill" {
		t.Errorf("Base = %q, want /gskill", nav.Base)
	}
	if len(nav.Groups) != len(RequiredGroups) {
		t.Fatalf("got %d groups, want %d", len(nav.Groups), len(RequiredGroups))
	}
	start := nav.Groups[0]
	if start.Pages[0].Path != "README.md" || start.Pages[0].Label != "Overview" {
		t.Errorf("object page = %+v, want README.md labelled Overview", start.Pages[0])
	}
	if got := nav.Groups[1].Pages[0]; got.Path != "how-to/a.md" || got.Label != "" {
		t.Errorf("string page = %+v, want how-to/a.md with no label", got)
	}
	if got := nav.Exclude[0]; got.Path != "design/**" || got.Reason == "" {
		t.Errorf("exclude = %+v, want design/** with a reason", got)
	}
}

func TestParseNav_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		old, new string
		want     string
	}{
		{
			name: "wrong base",
			old:  "base: /gskill", new: "base: /rune",
			want: "docs/nav.yaml: base must be /gskill",
		},
		{
			name: "exclusion without reason",
			old:  "    reason: internal design records\n", new: "",
			want: "docs/nav.yaml: exclude design/** needs a reason",
		},
		{
			name: "groups out of contract",
			old:  "group: Reference", new: "group: Concepts",
			want: "docs/nav.yaml: groups must be exactly: Start here, How-to guides, Reference, Explanation, Contributing",
		},
		{
			name: "page is not markdown",
			old:  "- how-to/a.md", new: "- how-to/a.txt",
			want: `docs/nav.yaml: group "How-to guides" page how-to/a.txt must be a .md path`,
		},
		{
			name: "link without url",
			old:  "url: https://github.com/glapsfun/gskill/releases", new: `url: ""`,
			want: `docs/nav.yaml: group "Contributing" has a link without a label or url`,
		},
		{
			name: "unknown field",
			old:  "base: /gskill", new: "base: /gskill\ncolour: red",
			want: "docs/nav.yaml:",
		},
		{
			name: "malformed yaml",
			old:  "groups:", new: "groups: [",
			want: "docs/nav.yaml:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(validNav, tt.old, tt.new, 1)
			if src == validNav {
				t.Fatalf("replacement %q did not apply", tt.old)
			}
			_, err := ParseNav([]byte(src), "docs/nav.yaml")
			if err == nil {
				t.Fatalf("ParseNav succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestMatchGlob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern, rel string
		want         bool
	}{
		{"design/**", "design/x.md", true},
		{"design/**", "design/a/b.md", true},
		{"design/**", "designs/x.md", false},
		{"*.md", "a.md", true},
		{"*.md", "a/b.md", false},
		{"examples/*/README.md", "examples/go/README.md", true},
		{"examples/*/README.md", "examples/go/deep/README.md", false},
		{"how-to/?.md", "how-to/a.md", true},
		{"how-to/a.md", "how-to/a.md", true},
		{"how-to/a.md", "how-to/ab.md", false},
		{"**/index.md", "index.md", true},
		{"**/index.md", "how-to/index.md", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+" "+tt.rel, func(t *testing.T) {
			t.Parallel()
			if got := matchGlob(tt.pattern, tt.rel); got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.rel, got, tt.want)
			}
		})
	}
}

func TestCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		nav    func(string) string
		files  map[string]string
		remove []string
		want   []string
	}{
		{
			name: "every page accounted for",
		},
		{
			name:  "unlisted page",
			files: map[string]string{"docs/how-to/b.md": "# B\n\nB.\n"},
			want:  []string{"docs/how-to/b.md is in neither docs/nav.yaml nor its exclude list"},
		},
		{
			name:   "listed page missing",
			remove: []string{"docs/explanation/e.md"},
			want:   []string{`docs/nav.yaml: group "Explanation" lists explanation/e.md, which does not exist`},
		},
		{
			name: "page listed twice",
			nav: func(s string) string {
				return strings.Replace(s, "      - reference/r.md\n", "      - reference/r.md\n      - how-to/a.md\n", 1)
			},
			want: []string{"docs/nav.yaml lists how-to/a.md more than once"},
		},
		{
			name: "page listed and excluded",
			nav: func(s string) string {
				return strings.Replace(s, "exclude:\n", "exclude:\n  - path: how-to/**\n    reason: drafts\n", 1)
			},
			want: []string{"docs/nav.yaml lists how-to/a.md, which exclude also matches"},
		},
		{
			name:   "exclusion matches nothing",
			remove: []string{"docs/design/x.md"},
			want:   []string{"docs/nav.yaml: exclude design/** matches nothing"},
		},
		{
			name: "page glob matches nothing",
			nav: func(s string) string {
				return strings.Replace(s, "      - how-to/a.md\n", "      - how-to/a.md\n      - guides/*.md\n", 1)
			},
			want: []string{`docs/nav.yaml: group "How-to guides" glob guides/*.md matches nothing`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := baseTree()
			for rel, content := range tt.files {
				files[rel] = content
			}
			for _, rel := range tt.remove {
				delete(files, rel)
			}
			navSrc := validNav
			if tt.nav != nil {
				navSrc = tt.nav(navSrc)
			}
			root := newTree(t, files)
			nav, err := ParseNav([]byte(navSrc), "docs/nav.yaml")
			if err != nil {
				t.Fatalf("ParseNav: %v", err)
			}

			got := joinErrs(Coverage(nav, filepath.Join(root, "docs")))
			if len(tt.want) == 0 && got != "" {
				t.Fatalf("Coverage reported errors:\n%s", got)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("Coverage errors:\n%s\nwant one containing %q", got, want)
				}
			}
		})
	}
}

func TestResolve_GlobsExpandSorted(t *testing.T) {
	t.Parallel()

	navSrc := strings.Replace(validNav, "      - how-to/a.md\n", "      - how-to/*.md\n", 1)
	nav, err := ParseNav([]byte(navSrc), "docs/nav.yaml")
	if err != nil {
		t.Fatalf("ParseNav: %v", err)
	}
	pages := []string{"README.md", "how-to/c.md", "how-to/a.md", "how-to/b.md", "reference/r.md", "explanation/e.md", "contributing/c.md", "design/x.md"}

	entries, errs := resolve(nav, pages)
	if len(errs) > 0 {
		t.Fatalf("resolve: %s", joinErrs(errs))
	}
	var howTo []string
	for _, e := range entries {
		if e.Group == "How-to guides" {
			howTo = append(howTo, e.Path)
		}
	}
	if got, want := strings.Join(howTo, ","), "how-to/a.md,how-to/b.md,how-to/c.md"; got != want {
		t.Errorf("glob expansion = %s, want %s", got, want)
	}
	if entries[0].Label != "Overview" {
		t.Errorf("first entry label = %q, want Overview", entries[0].Label)
	}
}

func TestScanDocs(t *testing.T) {
	t.Parallel()

	root := newTree(t, map[string]string{
		"docs/README.md":     "# R\n\nr\n",
		"docs/a/B.MD":        "# B\n\nb\n",
		"docs/img/d.svg":     "<svg/>",
		"docs/notes.txt":     "skip me",
		"docs/nav.yaml":      validNav,
		"docs/img/photo.png": "png",
	})
	pages, assets, err := scanDocs(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatalf("scanDocs: %v", err)
	}
	if got, want := strings.Join(pages, ","), "README.md,a/B.MD"; got != want {
		t.Errorf("pages = %s, want %s", got, want)
	}
	if got, want := strings.Join(assets, ","), "img/d.svg,img/photo.png"; got != want {
		t.Errorf("assets = %s, want %s", got, want)
	}

	if _, _, err := scanDocs(filepath.Join(root, "missing")); err == nil {
		t.Error("scanDocs on a missing dir succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}
}
