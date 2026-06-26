package metadata_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/metadata"
)

const validSkill = `---
name: kubernetes-expert
description: Kubernetes operational guidance
version: 2.1.3
license: MIT
compatibility: ">=1.0"
requires:
  skills: ["shell-scripting >=1.2.0"]
  commands: ["kubectl", "helm"]
  environment: ["KUBECONFIG"]
  mcp: []
---

# Body

Instructions here.
`

func TestParse_ValidFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := metadata.Parse([]byte(validSkill))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fm := doc.Frontmatter
	if fm.Name != "kubernetes-expert" {
		t.Errorf("Name = %q", fm.Name)
	}
	if fm.Description != "Kubernetes operational guidance" {
		t.Errorf("Description = %q", fm.Description)
	}
	if fm.Version != "2.1.3" || fm.License != "MIT" {
		t.Errorf("version/license = %q/%q", fm.Version, fm.License)
	}
	if len(fm.Requires.Commands) != 2 || fm.Requires.Commands[0] != "kubectl" {
		t.Errorf("requires.commands = %v", fm.Requires.Commands)
	}
	if len(fm.Requires.Skills) != 1 {
		t.Errorf("requires.skills = %v", fm.Requires.Skills)
	}
	if !strings.Contains(string(doc.Body), "Instructions here.") {
		t.Errorf("body not captured: %q", doc.Body)
	}
}

func TestParse_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"missing name":        "---\ndescription: d\n---\nbody\n",
		"missing description": "---\nname: foo\n---\nbody\n",
		"empty description":   "---\nname: foo\ndescription: \"\"\n---\nbody\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := metadata.Parse([]byte(content)); err == nil {
				t.Error("expected validation error")
			} else if !errors.Is(err, metadata.ErrInvalidFrontmatter) {
				t.Errorf("error = %v, want ErrInvalidFrontmatter", err)
			}
		})
	}
}

func TestParse_InvalidName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Kube_Expert", "UPPER", "has space", "-leading", "trailing-"} {
		content := "---\nname: " + name + "\ndescription: d\n---\nbody\n"
		if _, err := metadata.Parse([]byte(content)); err == nil {
			t.Errorf("name %q accepted, want rejection", name)
		}
	}
}

func TestParse_UnknownKeyWarnsButSucceeds(t *testing.T) {
	t.Parallel()

	content := "---\nname: foo\ndescription: d\nfuture_field: x\n---\nbody\n"
	doc, err := metadata.Parse([]byte(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Warnings) == 0 {
		t.Error("expected a warning for the unknown key")
	}
	found := false
	for _, w := range doc.Warnings {
		if strings.Contains(w, "future_field") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings %v do not mention future_field", doc.Warnings)
	}
}

func TestParse_MalformedYAML(t *testing.T) {
	t.Parallel()

	content := "---\nname: foo\n  bad: : indent\n---\nbody\n"
	if _, err := metadata.Parse([]byte(content)); err == nil {
		t.Error("expected error for malformed YAML")
	} else if !errors.Is(err, metadata.ErrInvalidFrontmatter) {
		t.Errorf("error = %v, want ErrInvalidFrontmatter", err)
	}
}

func TestParse_MissingDelimiters(t *testing.T) {
	t.Parallel()

	if _, err := metadata.Parse([]byte("no frontmatter here\n")); err == nil {
		t.Error("expected error for missing frontmatter delimiters")
	}
}

func TestParse_CompatibilityObject(t *testing.T) {
	t.Parallel()

	content := "---\nname: foo\ndescription: d\ncompatibility:\n  min: \"1.0\"\n---\nbody\n"
	doc, err := metadata.Parse([]byte(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Frontmatter.Compatibility == nil {
		t.Error("compatibility object not captured")
	}
}
