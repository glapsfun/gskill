package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// problemSource builds a source with a valid skill, an invalid skill, and a
// duplicated identity.
func problemSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(sp, body string) {
		dir := filepath.Join(root, filepath.FromSlash(sp))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/ok", validSkill("ok"))
	write("skills/broken", "---\nname: broken\n---\n# broken\n") // missing description
	write("skills/a/dup", validSkill("dup"))
	write("skills/b/dup", validSkill("dup"))
	return root
}

func TestSourceList_EnumeratesJSON(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)

	stdout, stderr, code := runGskill(t, proj, "--json", "source", "list", src)
	if code != 0 {
		t.Fatalf("source list exit %d: %s", code, stderr)
	}
	var items []struct {
		ID       string `json:"id"`
		RepoPath string `json:"repo_path"`
		Valid    bool   `json:"valid"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout)
	}
	if len(items) != 2 {
		t.Errorf("listed %d, want 2", len(items))
	}
}

func TestSourceInspect_ShowsOne(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)

	stdout, stderr, code := runGskill(t, proj, "source", "inspect", src, "--skill", "code-review")
	if code != 0 {
		t.Fatalf("source inspect exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "code-review") || !strings.Contains(stdout, "skills/code-review") {
		t.Errorf("inspect output missing details:\n%s", stdout)
	}
}

func TestSourceCheck_ExitsThreeOnProblems(t *testing.T) {
	t.Parallel()
	src := problemSource(t)
	proj := newProject(t)

	stdout, _, code := runGskill(t, proj, "source", "check", src)
	if code != 3 {
		t.Errorf("exit = %d, want 3 (problems found)", code)
	}
	if !strings.Contains(stdout, "broken") || !strings.Contains(stdout, "dup") {
		t.Errorf("check should report broken + dup:\n%s", stdout)
	}
}

func TestSourceCheck_CleanExitsZero(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "source", "check", src); code != 0 {
		t.Errorf("clean source check exit %d: %s", code, stderr)
	}
}

func TestSourceCommands_ReadOnly(t *testing.T) {
	t.Parallel()
	src := manySource(t, "code-review", "writing")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	before := snapshot(t, proj)

	for _, args := range [][]string{
		{"source", "list", src},
		{"source", "inspect", src, "--skill", "code-review"},
		{"source", "check", src},
	} {
		runGskill(t, proj, args...)
	}
	if after := snapshot(t, proj); after != before {
		t.Errorf("source commands modified the project:\nbefore=%s\nafter=%s", before, after)
	}
	// No agent dirs were created by inspection.
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills")); err == nil {
		t.Error("source commands must not install into agent dirs")
	}
}

// snapshot returns a stable description of the project's files for change detection.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(root, p)
			fmt.Fprintf(&b, "%s:%d\n", rel, info.Size())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
