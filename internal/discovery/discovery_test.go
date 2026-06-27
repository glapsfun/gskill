package discovery_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/metadata"
)

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover_SingleSkillAtRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, root, "alpha")

	skill, err := discovery.Discover(root, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if skill.Frontmatter.Name != "alpha" {
		t.Errorf("name = %q, want alpha", skill.Frontmatter.Name)
	}
	if skill.RelDir != "." {
		t.Errorf("RelDir = %q, want .", skill.RelDir)
	}
}

func TestDiscover_SingleSkillInSubdir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "alpha"), "alpha")

	skill, err := discovery.Discover(root, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if filepath.ToSlash(skill.RelDir) != "skills/alpha" {
		t.Errorf("RelDir = %q, want skills/alpha", skill.RelDir)
	}
}

func TestDiscover_ExplicitPathWins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha")
	writeSkill(t, filepath.Join(root, "b"), "bravo")

	skill, err := discovery.Discover(root, "b")
	if err != nil {
		t.Fatalf("Discover with explicit path: %v", err)
	}
	if skill.Frontmatter.Name != "bravo" {
		t.Errorf("name = %q, want bravo (explicit path must win)", skill.Frontmatter.Name)
	}
}

func TestDiscover_AmbiguousWithoutExplicitPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha")
	writeSkill(t, filepath.Join(root, "b"), "bravo")

	if _, err := discovery.Discover(root, ""); err == nil {
		t.Error("expected ambiguity error for multiple skills")
	} else if !errors.Is(err, discovery.ErrAmbiguousSkill) {
		t.Errorf("error = %v, want ErrAmbiguousSkill", err)
	}
}

func TestDiscover_NoSkill(t *testing.T) {
	t.Parallel()

	if _, err := discovery.Discover(t.TempDir(), ""); err == nil {
		t.Error("expected error when no SKILL.md present")
	} else if !errors.Is(err, discovery.ErrNoSkill) {
		t.Errorf("error = %v, want ErrNoSkill", err)
	}
}

func TestDiscover_ExplicitPathMissingSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha")

	if _, err := discovery.Discover(root, "does-not-exist"); err == nil {
		t.Error("expected ErrNoSkill for explicit path without SKILL.md")
	} else if !errors.Is(err, discovery.ErrNoSkill) {
		t.Errorf("error = %v, want ErrNoSkill", err)
	}
}

func TestDiscover_InvalidFrontmatterRejected(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"),
		[]byte("---\ndescription: no name\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := discovery.Discover(root, ""); err == nil {
		t.Error("expected validation error")
	} else if !errors.Is(err, metadata.ErrInvalidFrontmatter) {
		t.Errorf("error = %v, want ErrInvalidFrontmatter", err)
	}
}
