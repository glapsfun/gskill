package docsite

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSidebar(t *testing.T) {
	t.Parallel()

	nav, err := ParseNav([]byte(validNav), "docs/nav.yaml")
	if err != nil {
		t.Fatalf("ParseNav: %v", err)
	}
	entries := []NavEntry{
		{Path: "README.md", Label: "Overview", Group: "Start here"},
		{Path: "how-to/a.md", Group: "How-to guides"},
		{Path: "how-to/index.md", Label: "All how-to guides", Group: "How-to guides"},
		{Path: "reference/r.md", Group: "Reference"},
		{Path: "explanation/e.md", Group: "Explanation"},
		{Path: "contributing/c.md", Group: "Contributing"},
	}

	groups := Sidebar(nav, entries)
	data, err := json.Marshal(groups)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `[` +
		`{"label":"Start here","items":[{"label":"Overview","slug":"overview"}]},` +
		`{"label":"How-to guides","items":[{"slug":"how-to/a"},{"label":"All how-to guides","slug":"how-to"}]},` +
		`{"label":"Reference","items":[{"slug":"reference/r"}]},` +
		`{"label":"Explanation","items":[{"slug":"explanation/e"}]},` +
		`{"label":"Contributing","items":[{"slug":"contributing/c"},` +
		`{"label":"Releases","link":"https://github.com/glapsfun/gskill/releases"}]}` +
		`]`
	if got := string(data); got != want {
		t.Errorf("sidebar JSON =\n%s\nwant\n%s", got, want)
	}

	for _, g := range groups {
		if strings.TrimSpace(g.Label) == "" {
			t.Errorf("group with empty label: %+v", g)
		}
	}
}
