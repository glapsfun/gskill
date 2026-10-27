package docs

import (
	"testing"

	"github.com/glapsfun/gskill/internal/docsite"
)

// TestNavCoversEveryPage is the drift guard for the site's navigation: a
// Markdown page added under docs/ without being listed in docs/nav.yaml or
// excluded there fails here, under scripts/verify.sh, so it cannot silently go
// missing from the site.
func TestNavCoversEveryPage(t *testing.T) {
	t.Parallel()

	nav, err := docsite.ParseNav([]byte(readRepoFile(t, "docs", "nav.yaml")), "docs/nav.yaml")
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, err := range docsite.Coverage(nav, repoPath("docs")) {
		t.Error(err)
	}
}
