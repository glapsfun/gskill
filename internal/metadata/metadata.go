package metadata

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

// ErrInvalidFrontmatter is returned for malformed or schema-invalid SKILL.md
// frontmatter.
var ErrInvalidFrontmatter = errors.New("invalid frontmatter")

// nameRE matches lowercase-kebab skill names (FR-013).
var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// knownKeys are the top-level frontmatter keys understood by the v1 schema.
var knownKeys = map[string]bool{
	"name": true, "description": true, "version": true,
	"license": true, "compatibility": true, "requires": true,
}

// Frontmatter is the parsed SKILL.md YAML frontmatter (FR-012).
type Frontmatter struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Version       string   `json:"version,omitempty"`
	License       string   `json:"license,omitempty"`
	Compatibility any      `json:"compatibility,omitempty"`
	Requires      Requires `json:"requires,omitempty"`
}

// Requires holds declared needs that are recorded and surfaced, never resolved
// transitively (FR-032).
type Requires struct {
	Skills      []string `json:"skills,omitempty"`
	Commands    []string `json:"commands,omitempty"`
	Environment []string `json:"environment,omitempty"`
	MCP         []string `json:"mcp,omitempty"`
}

// Document is the result of parsing a SKILL.md: its frontmatter, the markdown
// body, and any non-fatal warnings.
type Document struct {
	Frontmatter Frontmatter
	Body        []byte
	Warnings    []string
}

// Validate checks the required-field and naming rules (FR-012, FR-013).
func (f Frontmatter) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("%w: missing required field 'name'", ErrInvalidFrontmatter)
	}
	if !nameRE.MatchString(f.Name) {
		return fmt.Errorf("%w: name %q must be lowercase kebab-case [a-z0-9-]", ErrInvalidFrontmatter, f.Name)
	}
	if strings.TrimSpace(f.Description) == "" {
		return fmt.Errorf("%w: missing required field 'description'", ErrInvalidFrontmatter)
	}
	return nil
}

// Parse extracts and validates the frontmatter from SKILL.md content, returning
// the document with its markdown body and any unknown-key warnings.
func Parse(content []byte) (Document, error) {
	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return Document{}, err
	}

	var f Frontmatter
	if uErr := yaml.Unmarshal([]byte(fm), &f); uErr != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrInvalidFrontmatter, uErr)
	}
	if vErr := f.Validate(); vErr != nil {
		return Document{}, vErr
	}

	var raw map[string]any
	if uErr := yaml.Unmarshal([]byte(fm), &raw); uErr != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrInvalidFrontmatter, uErr)
	}

	return Document{
		Frontmatter: f,
		Body:        []byte(body),
		Warnings:    unknownKeyWarnings(raw),
	}, nil
}

// splitFrontmatter separates the YAML frontmatter block from the markdown body.
func splitFrontmatter(content []byte) (frontmatter, body string, err error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", fmt.Errorf("%w: missing opening '---' delimiter", ErrInvalidFrontmatter)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), nil
		}
	}
	return "", "", fmt.Errorf("%w: missing closing '---' delimiter", ErrInvalidFrontmatter)
}

// unknownKeyWarnings reports a warning for each top-level key not in the v1
// schema (forward-compatible: warn, do not fail).
func unknownKeyWarnings(raw map[string]any) []string {
	var warnings []string
	for key := range raw {
		if !knownKeys[key] {
			warnings = append(warnings, fmt.Sprintf("unknown frontmatter key %q (ignored)", key))
		}
	}
	return warnings
}
