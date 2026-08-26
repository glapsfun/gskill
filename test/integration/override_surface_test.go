package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOverrideSurface_ListMarksOverridden: a customized skill is not the same
// artifact as its upstream, so `list` must say so. Otherwise the only way to
// discover that a skill was patched is to read the manifest.
func TestOverrideSurface_ListMarksOverridden(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	installWithOverride(t, proj)

	stdout, stderr, code := runGskill(t, proj, "list")
	if code != 0 {
		t.Fatalf("list: exit %d: %s", code, stderr)
	}
	// Assert the literal marker, not the substring "overrid": the temp project
	// path contains this test's own name, so a looser check matches the source
	// column and passes even with no marker rendered at all.
	var marked bool
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "demo") && strings.Contains(line, "(overridden)") {
			marked = true
		}
	}
	if !marked {
		t.Errorf("list does not mark the overridden skill on its row:\n%s", stdout)
	}
}

// TestOverrideSurface_ListJSON is contract assertion C3: the key is present for
// every skill, so a consumer branches on its value rather than on whether it
// exists.
func TestOverrideSurface_ListJSON(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	installWithOverride(t, proj)

	stdout, _, code := runGskill(t, proj, "list", "--json")
	if code != 0 {
		t.Fatalf("list --json: exit %d", code)
	}
	var doc struct {
		Skills []struct {
			Name       string `json:"name"`
			Overridden *bool  `json:"overridden"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("list --json is not valid JSON: %v\n%s", err, stdout)
	}
	if len(doc.Skills) == 0 {
		t.Fatalf("no skills listed:\n%s", stdout)
	}
	for _, s := range doc.Skills {
		if s.Overridden == nil {
			t.Errorf("skill %q omits the overridden key entirely", s.Name)
		} else if s.Name == "demo" && !*s.Overridden {
			t.Errorf("overridden skill reported as not overridden:\n%s", stdout)
		}
	}
}

// TestOverrideSurface_InfoShowsDeclaration: `info` is where a user looks to
// understand one skill, so it must show what was applied and both hashes.
func TestOverrideSurface_InfoShowsDeclaration(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	installWithOverride(t, proj)

	stdout, _, code := runGskill(t, proj, "info", "demo", "--json")
	if code != 0 {
		t.Fatalf("info --json: exit %d", code)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("info --json is not valid JSON: %v\n%s", err, stdout)
	}
	for _, key := range []string{"overridden", "override_digest", "base_hash"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("info --json omits %q:\n%s", key, stdout)
		}
	}
	if ov, ok := doc["override"]; !ok || ov == nil {
		t.Errorf("info --json does not show the resolved declaration:\n%s", stdout)
	}
}

// TestOverrideSurface_NonOverriddenStaysCompatible is the other half of C3: a
// skill with no override keeps the same shape, so adding these keys does not
// break an existing consumer.
func TestOverrideSurface_NonOverriddenStaysCompatible(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	stdout, _, _ := runGskill(t, proj, "info", "demo", "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("info --json invalid: %v", err)
	}
	if got, ok := doc["overridden"].(bool); !ok || got {
		t.Errorf("non-overridden skill reports overridden=%v:\n%s", doc["overridden"], stdout)
	}
	if got, ok := doc["override_digest"].(string); !ok || got != "" {
		t.Errorf("non-overridden skill has a digest: %v", doc["override_digest"])
	}
}
