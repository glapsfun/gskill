package docsite

import (
	"strings"
	"testing"
)

// testConfig returns the production Config with design/** excluded. root is the
// repository fixture used for target existence checks ("" disables them).
func testConfig(root string) Config {
	cfg := DefaultConfig()
	cfg.RepoRoot = root
	cfg.Excluded = func(rel string) bool { return matchGlob("design/**", rel) }
	return cfg
}

func linkTree() map[string]string {
	files := baseTree()
	files["docs/how-to/b.md"] = "# B\n\nB.\n"
	files["docs/how-to/index.md"] = "# How-to\n\nAll guides.\n"
	files[".github/workflows/release.yml"] = "name: Release\n"
	files["docs/img/d.svg"] = "<svg/>"
	files["docs/design/fig.png"] = "png"
	files["assets/logo.png"] = "png"
	return files
}

func TestRewriteLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, src, want string
	}{
		{"sibling page", "[b](b.md)", "[b](/gskill/how-to/b/)"},
		{"anchor kept", "[b](b.md#step-2)", "[b](/gskill/how-to/b/#step-2)"},
		{"docs root readme", "[home](../README.md)", "[home](/gskill/overview/)"},
		{"section index", "[all](index.md)", "[all](/gskill/how-to/)"},
		{"excluded page goes to github", "[x](../design/x.md)", "[x](" + BlobBase + "/docs/design/x.md)"},
		{"repository file", "[lic](../../LICENSE)", "[lic](" + BlobBase + "/LICENSE)"},
		{"repository file with anchor", "[wf](../../.github/workflows/release.yml#L1)", "[wf](" + BlobBase + "/.github/workflows/release.yml#L1)"},
		{"title attribute kept", `[b](b.md "Bee")`, `[b](/gskill/how-to/b/ "Bee")`},
		{"reference definition", "[b]: b.md", "[b]: /gskill/how-to/b/"},
		{"anchor only", "[top](#top)", "[top](#top)"},
		{"absolute url", "[x](https://example.com/a.md)", "[x](https://example.com/a.md)"},
		{"mailto", "[m](mailto:a@b.c)", "[m](mailto:a@b.c)"},
		{"site absolute", "[s](/gskill/x/)", "[s](/gskill/x/)"},
		{"inline code untouched", "`[b](b.md)` and [b](b.md)", "`[b](b.md)` and [b](/gskill/how-to/b/)"},
		{"fenced code untouched", "```md\n[b](b.md)\n```\n[b](b.md)", "```md\n[b](b.md)\n```\n[b](/gskill/how-to/b/)"},
		{"image is served from docs-assets", "![d](../img/d.svg)", "![d](/gskill/docs-assets/img/d.svg)"},
		{"link to an image in an excluded dir goes to github", "[f](../design/fig.png)", "[f](" + BlobBase + "/docs/design/fig.png)"},
		{"link to an image outside docs goes to github", "[l](../../assets/logo.png)", "[l](" + BlobBase + "/assets/logo.png)"},
		{"two links on a line", "[a](b.md) or [i](index.md)", "[a](/gskill/how-to/b/) or [i](/gskill/how-to/)"},
	}
	root := newTree(t, linkTree())
	cfg := testConfig(root)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RewriteLinks(tt.src, "how-to/a.md", cfg)
			if err != nil {
				t.Fatalf("RewriteLinks: %v", err)
			}
			if got != tt.want {
				t.Errorf("RewriteLinks(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestRewriteLinks_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, src, want string
	}{
		{"missing target", "[m](missing.md)", "how-to/a.md: link target missing.md does not exist"},
		{"missing repository file", "[m](../../nope.txt)", "how-to/a.md: link target ../../nope.txt does not exist"},
		{"escapes the repository", "[o](../../../outside.md)", "how-to/a.md: link target ../../../outside.md escapes the repository"},
		{"image outside docs", "![l](../../assets/logo.png)", "how-to/a.md: image ../../assets/logo.png is not published with the site"},
		{"image in an excluded dir", "![f](../design/fig.png)", "how-to/a.md: image ../design/fig.png is not published with the site"},
	}
	root := newTree(t, linkTree())
	cfg := testConfig(root)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := RewriteLinks(tt.src, "how-to/a.md", cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("RewriteLinks error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
