// nav.go parses docs/nav.yaml, the reviewable navigation manifest, and checks
// that every Markdown page under docs/ is either listed in it or excluded with
// a reason.

package docsite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// RequiredGroups are the sidebar sections, in order. The manifest must declare
// exactly these.
var RequiredGroups = []string{"Start here", "How-to guides", "Reference", "Explanation", "Contributing"}

// Nav is the parsed navigation manifest.
type Nav struct {
	Base    string           `json:"base"`
	Exclude []ExclusionEntry `json:"exclude"`
	Groups  []NavGroup       `json:"groups"`

	// name is how errors refer to the manifest, e.g. "docs/nav.yaml".
	name string
}

// ExclusionEntry keeps matching pages off the site. Reason is mandatory so a
// reviewer can see why a page is unpublished.
type ExclusionEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// NavGroup is one sidebar section.
type NavGroup struct {
	Group string    `json:"group"`
	Pages []NavPage `json:"pages"`
	Links []NavLink `json:"links"`
}

// NavPage is a docs-relative page path or glob, with an optional sidebar label.
// In YAML it is either a bare string or a {path, label} mapping.
type NavPage struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// NavLink is an external sidebar link.
type NavLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// NavEntry is one concrete published page in sidebar order, globs expanded.
type NavEntry struct {
	Path  string
	Label string
	Group string
}

// UnmarshalJSON accepts both the string and the mapping form of a page.
func (p *NavPage) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = NavPage{Path: s}
		return nil
	}
	type plain NavPage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var v plain
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("page must be a path or {path, label}: %w", err)
	}
	*p = NavPage(v)
	return nil
}

// ParseNav parses a manifest and validates its structure. name prefixes every
// error message so it points at the file a maintainer has to edit.
func ParseNav(src []byte, name string) (*Nav, error) {
	var nav Nav
	if err := yaml.UnmarshalStrict(src, &nav); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	nav.name = name

	var errs []error
	if nav.Base != BasePath {
		errs = append(errs, fmt.Errorf("%s: base must be %s", name, BasePath))
	}
	for _, ex := range nav.Exclude {
		if strings.TrimSpace(ex.Path) == "" {
			errs = append(errs, fmt.Errorf("%s: exclude entry without a path", name))
		} else if strings.TrimSpace(ex.Reason) == "" {
			errs = append(errs, fmt.Errorf("%s: exclude %s needs a reason", name, ex.Path))
		}
	}
	if !sameGroups(nav.Groups) {
		errs = append(errs, fmt.Errorf("%s: groups must be exactly: %s", name, strings.Join(RequiredGroups, ", ")))
	}
	for _, g := range nav.Groups {
		errs = append(errs, validateGroup(name, g)...)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &nav, nil
}

// sameGroups reports whether groups are RequiredGroups, in order.
func sameGroups(groups []NavGroup) bool {
	if len(groups) != len(RequiredGroups) {
		return false
	}
	for i, g := range groups {
		if g.Group != RequiredGroups[i] {
			return false
		}
	}
	return true
}

// validateGroup checks one group's pages and links.
func validateGroup(name string, g NavGroup) []error {
	var errs []error
	for _, p := range g.Pages {
		if !strings.EqualFold(path.Ext(p.Path), ".md") {
			errs = append(errs, fmt.Errorf("%s: group %q page %s must be a .md path", name, g.Group, p.Path))
		}
	}
	for _, l := range g.Links {
		if strings.TrimSpace(l.Label) == "" || strings.TrimSpace(l.URL) == "" {
			errs = append(errs, fmt.Errorf("%s: group %q has a link without a label or url", name, g.Group))
		}
	}
	return errs
}

// Excluded reports whether a docs-relative path matches any exclusion.
func (n *Nav) Excluded(rel string) bool {
	for _, ex := range n.Exclude {
		if matchGlob(ex.Path, rel) {
			return true
		}
	}
	return false
}

// Coverage checks the manifest against the Markdown pages actually present in
// docsDir and returns one error per problem: unlisted pages, missing or
// duplicate entries, pages both listed and excluded, and globs or exclusions
// that match nothing.
func Coverage(nav *Nav, docsDir string) []error {
	_, _, _, errs := check(nav, docsDir)
	return errs
}

// resolve expands the manifest against the pages present (docs-relative,
// sorted) into sidebar-ordered entries. Every problem found on the way is
// returned; the entries are still usable for reporting.
func resolve(nav *Nav, pages []string) ([]NavEntry, []error) {
	present := map[string]bool{}
	for _, p := range pages {
		present[p] = true
	}

	var errs []error
	for _, ex := range nav.Exclude {
		if !anyMatch(ex.Path, pages) {
			errs = append(errs, fmt.Errorf("%s: exclude %s matches nothing", nav.name, ex.Path))
		}
	}

	var entries []NavEntry
	seen := map[string]bool{}
	for _, g := range nav.Groups {
		for _, p := range g.Pages {
			matches := []string{p.Path}
			if isGlob(p.Path) {
				matches = globPages(p.Path, pages, nav)
				if len(matches) == 0 {
					errs = append(errs, fmt.Errorf("%s: group %q glob %s matches nothing", nav.name, g.Group, p.Path))
				}
			} else if !present[p.Path] {
				errs = append(errs, fmt.Errorf("%s: group %q lists %s, which does not exist", nav.name, g.Group, p.Path))
				continue
			}
			for _, rel := range matches {
				switch {
				case seen[rel]:
					errs = append(errs, fmt.Errorf("%s lists %s more than once", nav.name, rel))
					continue
				case nav.Excluded(rel):
					errs = append(errs, fmt.Errorf("%s lists %s, which exclude also matches", nav.name, rel))
					continue
				}
				seen[rel] = true
				entry := NavEntry{Path: rel, Group: g.Group}
				if !isGlob(p.Path) {
					entry.Label = p.Label
				}
				entries = append(entries, entry)
			}
		}
	}
	return entries, errs
}

// globPages returns the non-excluded pages a glob matches, sorted.
func globPages(pattern string, pages []string, nav *Nav) []string {
	var out []string
	for _, rel := range pages {
		if matchGlob(pattern, rel) && !nav.Excluded(rel) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// anyMatch reports whether pattern matches at least one of pages.
func anyMatch(pattern string, pages []string) bool {
	for _, rel := range pages {
		if matchGlob(pattern, rel) {
			return true
		}
	}
	return false
}

// isGlob reports whether a manifest path uses glob syntax.
func isGlob(p string) bool {
	return strings.ContainsAny(p, "*?[")
}

// matchGlob matches a slash path against a pattern where * and ? stay within
// one path segment and ** spans any number of segments.
func matchGlob(pattern, rel string) bool {
	re, err := globRegexp(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(rel)
}

// globRegexp translates a glob into an anchored regular expression.
func globRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// imageExts are the asset types copied from docs/ to the site.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true}

// scanDocs walks docsDir and returns the docs-relative Markdown pages and image
// assets it contains, each sorted.
func scanDocs(docsDir string) (pages, assets []string, err error) {
	err = filepath.WalkDir(docsDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(docsDir, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		ext := strings.ToLower(path.Ext(rel))
		switch {
		case ext == ".md":
			pages = append(pages, rel)
		case imageExts[ext]:
			assets = append(assets, rel)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("scan %s: %w", docsDir, err)
	}
	sort.Strings(pages)
	sort.Strings(assets)
	return pages, assets, nil
}
