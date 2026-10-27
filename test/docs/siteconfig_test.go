package docs

import (
	"regexp"
	"testing"

	"github.com/glapsfun/gskill/internal/docsite"
)

// TestAstroConfigMatchesGenerator keeps the site origin and base path in
// website/astro.config.mjs identical to the values cmd/gen-site rewrites links
// against. If they drift, every rewritten link 404s.
func TestAstroConfigMatchesGenerator(t *testing.T) {
	t.Parallel()

	config := readRepoFile(t, "website", "astro.config.mjs")
	tests := []struct {
		key, want string
	}{
		{"site", docsite.Site},
		{"base", docsite.BasePath},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			re := regexp.MustCompile(`(?m)^\s*` + tt.key + `:\s*'([^']*)'`)
			m := re.FindStringSubmatch(config)
			if m == nil {
				t.Fatalf("website/astro.config.mjs has no %s: '...' entry", tt.key)
			}
			if m[1] != tt.want {
				t.Errorf("astro.config.mjs %s = %q, internal/docsite has %q", tt.key, m[1], tt.want)
			}
		})
	}
}
