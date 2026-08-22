package installer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/overrides"
)

// writeInput writes a repo-relative override input file and returns its path.
func writeInput(t *testing.T, root, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestInstall_AppliesOverride is T025: what gets materialized and hashed is the
// post-override result, and BaseHash still records the upstream content.
func TestInstall_AppliesOverride(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	projectRoot := t.TempDir()
	writeInput(t, projectRoot, "gskill/demo/rules.md", "## House rules\nCite file:line.\n")

	req := localRequest(t, projectRoot, material, "demo")
	req.Override = overrides.Spec{Append: []string{"gskill/demo/rules.md"}}

	res, err := newInstaller(t).Install(context.Background(), req)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if res.BaseHash == "" {
		t.Error("BaseHash not recorded")
	}
	if res.BaseHash == res.ContentHash {
		t.Error("ContentHash must differ from BaseHash once an override applies")
	}

	got := readSkill(t, filepath.Join(projectRoot, ".agents", "skills", "demo", "SKILL.md"))
	if !strings.Contains(got, "House rules") {
		t.Errorf("override not applied to committed content:\n%s", got)
	}
}

// TestInstall_OverrideDoesNotMutateSource is the constraint that makes the
// staged copy mandatory: materialize returns the *shared* clone cache path (or
// the user's own local source directory). Transforming it in place would
// corrupt the cache for every other project on the machine.
func TestInstall_OverrideDoesNotMutateSource(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	before := readSkill(t, filepath.Join(material, "SKILL.md"))

	projectRoot := t.TempDir()
	writeInput(t, projectRoot, "gskill/demo/rules.md", "appended\n")

	req := localRequest(t, projectRoot, material, "demo")
	req.Override = overrides.Spec{Append: []string{"gskill/demo/rules.md"}}

	if _, err := newInstaller(t).Install(context.Background(), req); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if after := readSkill(t, filepath.Join(material, "SKILL.md")); after != before {
		t.Errorf("install mutated the source material:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if entries, err := os.ReadDir(material); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".gskill-override-") {
				t.Errorf("staging directory left behind in the source: %s", e.Name())
			}
		}
	}
}

// TestInstall_NoOverrideKeepsHashes is FR-008: with no override declared the
// base and content hashes agree, so spec 022 entries need no rewrite.
func TestInstall_NoOverrideKeepsHashes(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	projectRoot := t.TempDir()

	res, err := newInstaller(t).Install(context.Background(), localRequest(t, projectRoot, material, "demo"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.BaseHash != res.ContentHash {
		t.Errorf("BaseHash %q != ContentHash %q with no override declared", res.BaseHash, res.ContentHash)
	}
}

// TestInstall_OverrideFailureLeavesNothing: a failing override aborts the
// install without activating anything (FR-011).
func TestInstall_OverrideFailureLeavesNothing(t *testing.T) {
	t.Parallel()

	material := localSkill(t, "demo")
	projectRoot := t.TempDir()
	writeInput(t, projectRoot, "gskill/demo/bad.diff",
		"--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-nonexistent context\n+replacement\n")

	req := localRequest(t, projectRoot, material, "demo")
	req.Override = overrides.Spec{Patch: []string{"gskill/demo/bad.diff"}}

	if _, err := newInstaller(t).Install(context.Background(), req); err == nil {
		t.Fatal("install must fail when a declared patch does not apply")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".agents", "skills", "demo")); err == nil {
		t.Error("failed override still activated content")
	}
}

func readSkill(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test reads its own temp files
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
