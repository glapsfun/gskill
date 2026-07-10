package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddSingleSkill_JSONOutput(t *testing.T) {
	t.Parallel()

	src := localTreeSkill(t, "skills/foo", "foo")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	stdout, stderr, code := runGskill(t, proj, "--json", "add", src)
	if code != 0 {
		t.Fatalf("add --json exit %d: %s", code, stderr)
	}
	var got struct {
		Installed []struct {
			Name        string            `json:"name"`
			Path        string            `json:"path"`
			ContentHash string            `json:"content_hash"`
			Targets     map[string]string `json:"targets"`
		} `json:"installed"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("add --json output not valid JSON: %v\n%s", err, stdout)
	}
	if len(got.Installed) != 1 {
		t.Fatalf("installed = %d, want 1", len(got.Installed))
	}
	one := got.Installed[0]
	if one.Name != "foo" {
		t.Errorf("json name = %q, want foo", one.Name)
	}
	if one.Path != "skills/foo" {
		t.Errorf("json path = %q, want skills/foo", one.Path)
	}
	if one.ContentHash == "" {
		t.Error("json content_hash empty")
	}
	if _, ok := one.Targets["claude"]; !ok {
		t.Errorf("json targets missing claude: %v", one.Targets)
	}
}

// localTreeSkill creates a local source with one skill at the given in-repo
// subpath (forward-slash; "" for root) and returns the source root.
func localTreeSkill(t *testing.T, subpath, name string) string {
	t.Helper()

	root := t.TempDir()
	var dir string
	if subpath == "" {
		// A root skill's identity is the source directory's base name, so name
		// the source dir for a predictable root identity (FR-008/R2).
		root = filepath.Join(root, name)
		dir = root
	} else {
		dir = filepath.Join(root, filepath.FromSlash(subpath))
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(validSkill(name)), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAddSingleSkill_AnywhereRecordsPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		title, subpath, name, wantPath string
	}{
		{"root", "", "root-skill", ""},
		{"nested", "skills/foo", "foo", "skills/foo"},
		{"deep", "a/b/c", "c", "a/b/c"},
	}
	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			t.Parallel()

			src := localTreeSkill(t, c.subpath, c.name)
			proj := newProject(t)
			if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
				t.Fatalf("init: %s", stderr)
			}

			_, stderr, code := runGskill(t, proj, "add", src)
			if code != 0 {
				t.Fatalf("add exit %d: %s", code, stderr)
			}

			// Installed under the folder-derived identity.
			installed := filepath.Join(proj, ".claude", "skills", c.name, "SKILL.md")
			if _, err := os.Stat(installed); err != nil {
				t.Errorf("skill not installed at %s: %v", installed, err)
			}

			// Lockfile records the skill and its in-repo path.
			lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
			if c.wantPath != "" && !strings.Contains(lock, c.wantPath) {
				t.Errorf("lockfile missing path %q:\n%s", c.wantPath, lock)
			}
		})
	}
}

func TestAddSingleSkill_IgnoredDirsUnaffected(t *testing.T) {
	t.Parallel()

	// A single real skill plus a decoy SKILL.md inside node_modules.
	src := localTreeSkill(t, "skills/real", "real")
	decoy := filepath.Join(src, "node_modules", "pkg")
	if err := os.MkdirAll(decoy, 0o750); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(decoy, "SKILL.md"), []byte(validSkill("decoy")), 0o600)

	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	// Exactly one skill is discovered (decoy pruned), so add installs without a selector.
	if _, stderr, code := runGskill(t, proj, "add", src); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "real", "SKILL.md")); err != nil {
		t.Errorf("real skill not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "decoy")); err == nil {
		t.Error("decoy skill from node_modules should not have been installed")
	}
}
