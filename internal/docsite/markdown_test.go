package docsite

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParsePage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		src       string
		wantTitle string
		wantDesc  string
		wantBody  string
	}{
		{
			name:      "title and first paragraph",
			src:       "# Title\n\nFirst paragraph here.\n\n## Next\n",
			wantTitle: "Title",
			wantDesc:  "First paragraph here.",
			wantBody:  "First paragraph here.\n\n## Next\n",
		},
		{
			name:      "leading html comment skipped",
			src:       "<!-- a note\nover two lines -->\n\n# Real title\n\nBody text.\n",
			wantTitle: "Real title",
			wantDesc:  "Body text.",
			wantBody:  "Body text.\n",
		},
		{
			name:      "inline markdown stripped from title",
			src:       "# The `skills-lock.json` [schema](x.md) **now**\n\nText.\n",
			wantTitle: "The skills-lock.json schema now",
			wantDesc:  "Text.",
		},
		{
			name:      "description skips non-prose blocks",
			src:       "# T\n\n> A quote.\n\n```sh\ncode line\n```\n\n- an item\n\n| a | b |\n\nReal **prose** with [a link](x.md) and `code`.\nSecond line.\n\nLater paragraph.\n",
			wantTitle: "T",
			wantDesc:  "Real prose with a link and code. Second line.",
		},
		{
			name:      "heading inside a code fence is not the title",
			src:       "```sh\n# not a title\n```\n\n# Real\n\nPara.\n",
			wantTitle: "Real",
			wantDesc:  "Para.",
		},
		{
			name:      "code span contents kept verbatim",
			src:       "# T\n\nCommitted at `.agents/skills/<name>/`, with `a*b*`, ``__init__`` and <em>html</em>.\n",
			wantTitle: "T",
			wantDesc:  "Committed at .agents/skills/<name>/, with a*b*, __init__ and html.",
		},
		{
			name:      "less-than outside a tag kept",
			src:       "# T\n\nWhen a < b and b > c.\n",
			wantTitle: "T",
			wantDesc:  "When a < b and b > c.",
		},
		{
			name:      "trailing hash in the title kept",
			src:       "# Use GSKILL with C#\n\nText.\n",
			wantTitle: "Use GSKILL with C#",
			wantDesc:  "Text.",
		},
		{
			name:      "closing hashes dropped from the title",
			src:       "# Closed title ##  \n\nText.\n",
			wantTitle: "Closed title",
			wantDesc:  "Text.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			page, err := ParsePage(tt.src, "how-to/a.md", testConfig(""))
			if err != nil {
				t.Fatalf("ParsePage: %v", err)
			}
			if page.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", page.Title, tt.wantTitle)
			}
			if page.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", page.Description, tt.wantDesc)
			}
			if tt.wantBody != "" && page.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", page.Body, tt.wantBody)
			}
			if want := EditBase + "/how-to/a.md"; page.EditURL != want {
				t.Errorf("EditURL = %q, want %q", page.EditURL, want)
			}
		})
	}
}

func TestParsePage_DescriptionTrimmed(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("alpha beta ", 30)
	page, err := ParsePage("# T\n\n"+long+"\n", "a.md", testConfig(""))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if n := utf8.RuneCountInString(page.Description); n > maxDescription {
		t.Errorf("description has %d runes, want at most %d", n, maxDescription)
	}
	if !strings.HasSuffix(page.Description, "…") {
		t.Errorf("trimmed description %q does not end with an ellipsis", page.Description)
	}
	if !strings.HasPrefix(page.Description, "alpha beta alpha") {
		t.Errorf("trimmed description %q lost its start", page.Description)
	}
}

func TestParsePage_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, src, want string
	}{
		{name: "no title", src: "Just text.\n", want: "a.md: no # title"},
		{name: "no prose paragraph", src: "# T\n\n```sh\nx\n```\n", want: "a.md: no prose paragraph to use as the description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParsePage(tt.src, "a.md", testConfig(""))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ParsePage error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestPage_Frontmatter(t *testing.T) {
	t.Parallel()

	page := Page{Title: `A: "quoted"`, Description: "Uses <tags> & more.", EditURL: "https://e/x.md"}
	want := "---\ntitle: \"A: \\\"quoted\\\"\"\ndescription: \"Uses <tags> & more.\"\neditUrl: \"https://e/x.md\"\n---\n"
	if got := page.Frontmatter(); got != want {
		t.Errorf("Frontmatter() =\n%s\nwant\n%s", got, want)
	}

	page.EditURL = ""
	if got := page.Frontmatter(); !strings.Contains(got, "\neditUrl: false\n") {
		t.Errorf("Frontmatter() without EditURL =\n%s\nwant editUrl: false", got)
	}
}

func TestTransform(t *testing.T) {
	t.Parallel()

	root := newTree(t, baseTree())
	out, err := Transform(baseTree()["docs/how-to/a.md"], "how-to/a.md", testConfig(root))
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	for _, want := range []string{
		"title: \"Do A\"",
		"description: \"How to do A. See the reference.\"",
		"[the reference](/gskill/reference/r/#flags)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Transform output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "# Do A") {
		t.Errorf("Transform kept the H1:\n%s", out)
	}
}

func TestParsePage_GeneratedReference(t *testing.T) {
	t.Parallel()

	src := "<!-- GENERATED by cmd/gen-reference; DO NOT EDIT. Run: go run ./cmd/gen-reference -->\n\n" +
		"# Command reference\n\nEvery `gskill` command and its flags.\n\n## `add`\n"
	page, err := ParsePage(src, "reference/commands.md", testConfig(""))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if page.EditURL != "" {
		t.Errorf("EditURL = %q, want none for a generated page", page.EditURL)
	}
	want := ":::note\nThis page is generated from the CLI. Do not edit it by hand; run\n" +
		"`go run ./cmd/gen-reference` and commit the result.\n:::\n\n" +
		"Every `gskill` command and its flags.\n"
	if !strings.HasPrefix(page.Body, want) {
		t.Errorf("Body =\n%s\nwant it to start with\n%s", page.Body, want)
	}
	if page.Description != "Every gskill command and its flags." {
		t.Errorf("Description = %q", page.Description)
	}
	if !strings.Contains(page.Frontmatter(), "\neditUrl: false\n") {
		t.Errorf("Frontmatter lacks editUrl: false:\n%s", page.Frontmatter())
	}
}
