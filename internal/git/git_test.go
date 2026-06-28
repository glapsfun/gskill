package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/git"
)

// fixtureRepo creates a local git repo with one commit tagged v1.0.0 and returns
// its path and the commit SHA.
func fixtureRepo(t *testing.T) (repo, commit string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	run("init", "--quiet", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"),
		[]byte("---\nname: demo\ndescription: d\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "initial")
	run("tag", "v1.0.0")
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	commit = string(out[:40])
	return repo, commit
}

func TestSystemRunner_LsRemoteTags(t *testing.T) {
	t.Parallel()

	repo, commit := fixtureRepo(t)
	r := git.NewSystemRunner()

	tags, err := r.LsRemoteTags(context.Background(), repo)
	if err != nil {
		t.Fatalf("LsRemoteTags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("got %d tags, want 1: %v", len(tags), tags)
	}
	if tags[0].Name != "v1.0.0" {
		t.Errorf("tag name = %q, want v1.0.0", tags[0].Name)
	}
	if tags[0].Commit != commit {
		t.Errorf("tag commit = %q, want %q", tags[0].Commit, commit)
	}
}

func TestSystemRunner_ResolveRef(t *testing.T) {
	t.Parallel()

	repo, commit := fixtureRepo(t)
	r := git.NewSystemRunner()

	got, err := r.ResolveRef(context.Background(), repo, "v1.0.0")
	if err != nil {
		t.Fatalf("ResolveRef tag: %v", err)
	}
	if got != commit {
		t.Errorf("ResolveRef(v1.0.0) = %q, want %q", got, commit)
	}

	got, err = r.ResolveRef(context.Background(), repo, commit)
	if err != nil {
		t.Fatalf("ResolveRef sha: %v", err)
	}
	if got != commit {
		t.Errorf("ResolveRef(sha) = %q, want %q", got, commit)
	}
}

func TestSystemRunner_ResolveRefPrefersBranchOverSameNamedTag(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	revParse := func(rev string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", "rev-parse", rev)
		cmd.Dir = repo
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("rev-parse %s: %v", rev, err)
		}
		return string(out[:40])
	}

	run("init", "--quiet", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "first")
	// Tag "shared" points at the first commit.
	run("tag", "shared")
	tagCommit := revParse("HEAD")

	// A "shared" branch advances to a second, different commit.
	run("checkout", "--quiet", "-b", "shared")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "second")
	branchCommit := revParse("HEAD")

	if branchCommit == tagCommit {
		t.Fatal("test setup: branch and tag commits must differ")
	}

	got, err := git.NewSystemRunner().ResolveRef(context.Background(), repo, "shared")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != branchCommit {
		t.Errorf("ResolveRef(shared) = %q, want branch head %q (not tag %q)", got, branchCommit, tagCommit)
	}
}

func TestSystemRunner_FetchCommit(t *testing.T) {
	t.Parallel()

	repo, commit := fixtureRepo(t)
	r := git.NewSystemRunner()

	dest := filepath.Join(t.TempDir(), "out")
	if err := r.FetchCommit(context.Background(), repo, commit, dest); err != nil {
		t.Fatalf("FetchCommit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git should be removed from fetched material, stat err=%v", err)
	}
}

func TestSystemRunner_UnavailableSourceErrors(t *testing.T) {
	t.Parallel()

	r := git.NewSystemRunner()
	_, err := r.LsRemoteTags(context.Background(), filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Error("expected error for nonexistent repo")
	}
}
