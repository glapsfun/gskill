package docsite

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rel, want string
	}{
		{"README.md", "overview"},
		{"installation.md", "installation"},
		{"how-to/index.md", "how-to"},
		{"how-to/README.md", "how-to"},
		{"how-to/add-a-git-skill.md", "how-to/add-a-git-skill"},
		{"how-to/Add-A-Git-Skill.MD", "how-to/add-a-git-skill"},
		{"ci-cd.md", "ci-cd"},
		{"index.md", ""},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			t.Parallel()
			if got := Slug(tt.rel); got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.rel, got, tt.want)
			}
		})
	}
}

func TestCheckSlugs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pages []string
		want  string
	}{
		{name: "distinct", pages: []string{"a.md", "how-to/b.md", "README.md"}},
		{name: "index and readme collide", pages: []string{"how-to/index.md", "how-to/README.md"}, want: "how-to/index.md and how-to/README.md both map to /gskill/how-to/"},
		{name: "case collision", pages: []string{"A.md", "a.md"}, want: "A.md and a.md both map to /gskill/a/"},
		{name: "root index takes the landing page", pages: []string{"index.md"}, want: "index.md maps to the site root, which belongs to the landing page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkSlugs(tt.pages)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("checkSlugs: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("checkSlugs error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
