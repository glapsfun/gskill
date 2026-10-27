package docs

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/docsite"
	"github.com/glapsfun/gskill/internal/metadata"
)

// TestReadmeInstallRegionMatchesDocs is the drift guard for the install
// commands: the README's region must be byte-identical to the one in
// docs/installation.md. Fix by running `go run ./cmd/gen-site`.
func TestReadmeInstallRegionMatchesDocs(t *testing.T) {
	t.Parallel()

	docsRegion, err := docsite.InstallRegion(readRepoFile(t, "docs", "installation.md"))
	if err != nil {
		t.Fatalf("docs/installation.md: %v", err)
	}
	readmeRegion, err := docsite.InstallRegion(readRepoFile(t, "README.md"))
	if err != nil {
		t.Fatalf("README.md: %v", err)
	}
	if readmeRegion != docsRegion {
		t.Errorf("README.md install region differs from docs/installation.md; run: go run ./cmd/gen-site")
	}
	if _, err := docsite.ParseInstall(readRepoFile(t, "docs", "installation.md")); err != nil {
		t.Errorf("docs/installation.md: %v", err)
	}
}

// TestExampleSkill checks the skill every runnable example installs is present
// and valid, so a pasted example cannot fail on the skill itself.
func TestExampleSkill(t *testing.T) {
	t.Parallel()

	doc, err := metadata.Parse([]byte(readRepoFile(t, "examples", "skills", "hello-gskill", "SKILL.md")))
	if err != nil {
		t.Fatalf("examples/skills/hello-gskill/SKILL.md: %v", err)
	}
	if doc.Frontmatter.Name != "hello-gskill" {
		t.Errorf("name = %q, want hello-gskill", doc.Frontmatter.Name)
	}
	if strings.TrimSpace(doc.Frontmatter.Description) == "" {
		t.Error("description is empty")
	}
}

// TestRunnableExamplesUseExampleSkill checks every copy-paste example that adds
// a skill points at the project's example skill, not a placeholder.
func TestRunnableExamplesUseExampleSkill(t *testing.T) {
	t.Parallel()

	readmeRegion, err := docsite.InstallRegion(readRepoFile(t, "README.md"))
	if err != nil {
		t.Fatalf("README.md: %v", err)
	}
	docsRegion, err := docsite.InstallRegion(readRepoFile(t, "docs", "installation.md"))
	if err != nil {
		t.Fatalf("docs/installation.md: %v", err)
	}
	sources := map[string]string{
		"README.md install region":            readmeRegion,
		"docs/installation.md install region": docsRegion,
		"website/src/landing/example.sh":      readRepoFile(t, "website", "src", "landing", "example.sh"),
	}
	for name, text := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(text, docsite.ExampleSkillSource) {
				t.Errorf("%s does not use %s", name, docsite.ExampleSkillSource)
			}
		})
	}
}
