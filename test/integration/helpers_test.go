package integration_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/testutil"
)

// newApp builds an App with a discard logger for tests.
func newApp() *app.App {
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// runGskill runs the CLI against root and returns stdout, stderr, and exit code.
func runGskill(t *testing.T, root string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runGskillWithApp(t, newApp(), root, args...)
}

// runGskillWithApp runs the CLI against root using a caller-provided App.
func runGskillWithApp(t *testing.T, a *app.App, root string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	full := append([]string{"-C", root}, args...)
	var out, errb bytes.Buffer
	code = cli.Run(context.Background(), full, &out, &errb, a)
	return out.String(), errb.String(), code
}

// readFile reads a project file, failing the test on error.
func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// localSkillDir creates a plain (non-git) local skill directory whose folder
// name is the skill identity (folder-identity model). The returned path is the
// skill directory itself, so `add <dir>` discovers a single root skill keyed by
// name.
func localSkillDir(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(validSkill(name)), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// skillFolder derives the folder name for a skill body: its frontmatter name,
// or "skill" when absent. Skills live in a folder named for their identity.
func skillFolder(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "name:") {
			if n := strings.TrimSpace(strings.TrimPrefix(line, "name:")); n != "" {
				return n
			}
		}
	}
	return "skill"
}

// newProject creates a project dir with a Claude Code marker so detection works.
func newProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	return root
}

// gitRepo creates a local git repo with a skill in a subdir named for the
// skill's identity (its frontmatter name, or "skill" when absent) containing
// skillBody, an initial commit, and the given tags. It returns the repo path.
func gitRepo(t *testing.T, skillBody string, tags ...string) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	gitRun(t, repo, "init", "--quiet", "-b", "main")

	skillDir := filepath.Join(repo, skillFolder(skillBody))
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillBody), 0o600); err != nil {
		t.Fatal(err)
	}

	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "--quiet", "-m", "initial")
	for _, tag := range tags {
		gitRun(t, repo, "tag", tag)
	}
	return repo
}

// gitRun runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// validSkill returns a valid SKILL.md body for a skill named name.
func validSkill(name string) string {
	return "---\nname: " + name + "\ndescription: a demo skill\n---\n# " + name + "\n"
}
