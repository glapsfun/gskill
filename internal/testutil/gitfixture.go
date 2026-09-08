package testutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// InitSkillRepo creates a local git repository holding one skill directory
// named name with body as its SKILL.md, an initial commit, and the given tags.
// It returns the repository path and skips the test when git is unavailable.
func InitSkillRepo(t *testing.T, name, body string, tags ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	GitRun(t, repo, "init", "--quiet", "-b", "main")
	writeSkill(t, repo, name, body)
	GitRun(t, repo, "add", ".")
	GitRun(t, repo, "commit", "--quiet", "-m", "initial")
	for _, tag := range tags {
		GitRun(t, repo, "tag", tag)
	}
	return repo
}

// PublishVersion replaces the skill's SKILL.md with body, commits, and tags the
// commit — the "upstream publishes a newer release" step of a lifecycle test.
func PublishVersion(t *testing.T, repo, name, body, tag string) {
	t.Helper()
	PublishCommit(t, repo, name, body)
	GitRun(t, repo, "tag", tag)
}

// PublishCommit replaces the skill's SKILL.md with body and commits it without
// tagging, advancing the branch head. It returns the new commit SHA.
func PublishCommit(t *testing.T, repo, name, body string) string {
	t.Helper()
	writeSkill(t, repo, name, body)
	GitRun(t, repo, "add", ".")
	GitRun(t, repo, "commit", "--quiet", "-m", "publish")
	return GitOutput(t, repo, "rev-parse", "HEAD")
}

// SkillBody returns a valid SKILL.md for name whose heading carries marker, so
// installed content can be told apart across versions.
func SkillBody(name, marker string) string {
	return "---\nname: " + name + "\ndescription: " + name + " " + marker + "\n---\n# " + name + " " + marker + "\n"
}

// GitRun runs a git command in dir with a scrubbed environment, failing the
// test on error.
func GitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := gitCmd(dir, args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// GitOutput runs a git command in dir and returns its trimmed stdout.
func GitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(dir, args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = GitEnv(
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	return cmd
}

func writeSkill(t *testing.T, repo, name, body string) {
	t.Helper()
	dir := filepath.Join(repo, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
