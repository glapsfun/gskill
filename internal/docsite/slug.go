// slug.go maps docs-relative Markdown paths to site slugs. The slug decides
// both the output file Starlight serves and every rewritten link, so the two
// cannot disagree.

package docsite

import (
	"fmt"
	"path"
	"strings"
)

// Slug returns the site slug for a docs-relative page: the path without its
// .md extension, lowercased. The docs root README.md becomes "overview" (the
// site root belongs to the landing page), a README.md or index.md inside a
// directory becomes that directory, and a root index.md maps to "".
func Slug(rel string) string {
	rel = strings.ToLower(rel)
	base := strings.TrimSuffix(rel, path.Ext(rel))
	dir, file := path.Split(base)
	dir = strings.TrimSuffix(dir, "/")

	switch {
	case dir == "" && file == "readme":
		return "overview"
	case file == "index" || file == "readme":
		return dir
	default:
		return base
	}
}

// checkSlugs fails when two pages map to the same slug, or when a page claims
// the site root.
func checkSlugs(pages []string) error {
	owner := map[string]string{}
	for _, rel := range pages {
		slug := Slug(rel)
		if slug == "" {
			return fmt.Errorf("%s maps to the site root, which belongs to the landing page", rel)
		}
		if first, ok := owner[slug]; ok {
			return fmt.Errorf("%s and %s both map to %s/%s/", first, rel, BasePath, slug)
		}
		owner[slug] = rel
	}
	return nil
}
