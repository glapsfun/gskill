// sidebar.go builds the Starlight sidebar from the manifest. The JSON lands in
// website/src/generated/sidebar.json, which astro.config.mjs reads.

package docsite

// SidebarGroup is one Starlight sidebar group.
type SidebarGroup struct {
	Label string        `json:"label"`
	Items []SidebarItem `json:"items"`
}

// SidebarItem is either a page, referenced by Slug so Starlight resolves the
// base path and fails the build on a missing page, or an external Link. Label
// overrides the page title when set.
type SidebarItem struct {
	Label string `json:"label,omitempty"`
	Slug  string `json:"slug,omitempty"`
	Link  string `json:"link,omitempty"`
}

// Sidebar groups resolved entries under the manifest's groups, in manifest
// order, and appends each group's external links.
func Sidebar(nav *Nav, entries []NavEntry) []SidebarGroup {
	groups := make([]SidebarGroup, 0, len(nav.Groups))
	for _, g := range nav.Groups {
		group := SidebarGroup{Label: g.Group, Items: []SidebarItem{}}
		for _, e := range entries {
			if e.Group == g.Group {
				group.Items = append(group.Items, SidebarItem{Label: e.Label, Slug: Slug(e.Path)})
			}
		}
		for _, l := range g.Links {
			group.Items = append(group.Items, SidebarItem{Label: l.Label, Link: l.URL})
		}
		groups = append(groups, group)
	}
	return groups
}
