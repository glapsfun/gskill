package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/testutil"
)

// updGit runs a git command in dir.
func updGit(t *testing.T, dir string, args ...string) {
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

// updRepo builds a git repo holding one skill (folder identity) tagged v1.0.0.
func updRepo(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	dir := filepath.Join(repo, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + " (" + t.Name() + ")\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	updGit(t, repo, "init", "--quiet", "-b", "main")
	updGit(t, repo, "add", ".")
	updGit(t, repo, "commit", "--quiet", "-m", "initial")
	updGit(t, repo, "tag", "v1.0.0")
	return repo
}

// publish commits a change to name's skill in repo and tags it.
func publish(t *testing.T, repo, name, tag string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: updated\n---\n# " + name + " " + tag + "\n"
	if err := os.WriteFile(filepath.Join(repo, name, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	updGit(t, repo, "add", ".")
	updGit(t, repo, "commit", "--quiet", "-m", tag)
	updGit(t, repo, "tag", tag)
}

// updApp builds an App with a private home.
func updApp(t *testing.T) *app.App {
	t.Helper()
	return app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: filepath.Join(t.TempDir(), "home"),
	})
}

// updateProject adds alpha and beta (both ^1.0.0) and publishes v1.1.0 in
// each repo, so both start actionable.
func updateProject(t *testing.T) (a *app.App, root, repoA, repoB string) {
	t.Helper()
	a = updApp(t)
	root = t.TempDir()
	repoA, repoB = updRepo(t, "alpha"), updRepo(t, "beta")
	for _, repo := range []string{repoA, repoB} {
		if _, err := a.Add(context.Background(), app.AddRequest{
			Root: root, Source: repo, Version: "^1.0.0", All: true, Agents: []string{"claude"},
		}); err != nil {
			t.Fatalf("add %s: %v", repo, err)
		}
	}
	publish(t, repoA, "alpha", "v1.1.0")
	publish(t, repoB, "beta", "v1.1.0")
	return a, root, repoA, repoB
}

// Versions used by the two-repo update fixture.
const (
	ver100 = "1.0.0"
	ver110 = "1.1.0"
)

// lockRecord loads name's typed record from root's lockfile.
func lockRecord(t *testing.T, root, name string) skillslock.Record {
	t.Helper()
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := l.Entry(name)
	if !ok {
		t.Fatalf("lock has no entry %q", name)
	}
	return skillslock.ToRecord(name, e)
}

func TestUpdate_PerSkillIsolationContinuesOnFailure(t *testing.T) {
	t.Parallel()

	a, root, _, repoB := updateProject(t)
	// Make beta's source unfetchable after discovery published its update.
	if err := os.RemoveAll(repoB); err != nil {
		t.Fatal(err)
	}

	res, err := a.Update(context.Background(), app.UpdateRequest{Root: root})
	if !errors.Is(err, errs.ErrPartialInstall) {
		t.Fatalf("err = %v, want partial-install sentinel", err)
	}
	if res.Updated != 1 || res.Failed != 1 {
		t.Fatalf("counts = %+v, want 1 updated / 1 failed", res)
	}
	assertIsolationRows(t, res.Skills)

	// Alpha advanced; beta stayed exactly at its pre-run lock state.
	if got := lockRecord(t, root, "alpha").Resolved.Version; got != ver110 {
		t.Errorf("alpha lock version = %q, want 1.1.0", got)
	}
	if got := lockRecord(t, root, "beta").Resolved.Version; got != ver100 {
		t.Errorf("beta lock version = %q, want untouched 1.0.0", got)
	}
}

// assertIsolationRows checks the two-skill isolation outcome: alpha updated
// with its transition, beta failed with a reason.
func assertIsolationRows(t *testing.T, skills []app.UpdateSkillResult) {
	t.Helper()
	for _, s := range skills {
		switch s.Name {
		case "alpha":
			if s.Outcome != app.UpdateOutcomeUpdated || s.From != ver100 || s.To != ver110 {
				t.Errorf("alpha = %+v, want updated 1.0.0 -> 1.1.0", s)
			}
		case "beta":
			if s.Outcome != app.UpdateOutcomeFailed || s.Reason == "" {
				t.Errorf("beta = %+v, want failed with reason", s)
			}
		}
	}
}

func TestUpdate_DryRunFailuresDoNotExitPartial(t *testing.T) {
	t.Parallel()

	a, root, _, repoB := updateProject(t)
	if err := os.RemoveAll(repoB); err != nil {
		t.Fatal(err)
	}

	// A dry run over broken state reports the failure row but never returns
	// the partial-installation error: nothing was attempted or written, so
	// the exit contract stays 0.
	dry, err := a.Update(context.Background(), app.UpdateRequest{Root: root, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run err = %v, want nil despite the failed skill", err)
	}
	if dry.Failed != 1 {
		t.Errorf("dry-run counts = %+v, want the failure still reported", dry)
	}
}

func TestUpdate_BranchReportsCommitTransition(t *testing.T) {
	t.Parallel()

	a := updApp(t)
	root := t.TempDir()
	repo := updRepo(t, "edge")
	if _, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: repo, Ref: "main", All: true, Agents: []string{"claude"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	publish(t, repo, "edge", "v1.1.0") // moves the branch head

	res, err := a.Update(context.Background(), app.UpdateRequest{Root: root, Names: []string{"edge"}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	s := res.Skills[0]
	if s.Outcome != app.UpdateOutcomeUpdated {
		t.Fatalf("edge = %+v, want updated", s)
	}
	// Both sides of the transition are short commit SHAs — the branch name
	// never changes and would hide what was actually installed (FR-015).
	if len(s.From) != 12 || len(s.To) != 12 || s.From == s.To || s.To == "main" {
		t.Errorf("transition = %q -> %q, want distinct 12-char commit SHAs", s.From, s.To)
	}
}

func TestUpdate_NamedReportsTransition(t *testing.T) {
	t.Parallel()

	a, root, _, _ := updateProject(t)

	res, err := a.Update(context.Background(), app.UpdateRequest{Root: root, Names: []string{"alpha"}})
	if err != nil {
		t.Fatalf("update alpha: %v", err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Outcome != app.UpdateOutcomeUpdated ||
		res.Skills[0].From != ver100 || res.Skills[0].To != ver110 {
		t.Fatalf("res = %+v, want alpha updated 1.0.0 -> 1.1.0", res.Skills)
	}
	// Only the named skill moved (FR-011).
	if got := lockRecord(t, root, "beta").Resolved.Version; got != ver100 {
		t.Errorf("beta lock version = %q, want untouched 1.0.0", got)
	}
}

func TestUpdate_NamedNoOpIsHonest(t *testing.T) {
	t.Parallel()

	a, root, _, _ := updateProject(t)
	if _, err := a.Update(context.Background(), app.UpdateRequest{Root: root, Names: []string{"alpha"}}); err != nil {
		t.Fatalf("first update: %v", err)
	}

	// Re-running the same name is an honest no-op, not a false success —
	// also the stale-selection path: a name chosen earlier that is no longer
	// actionable under the current lock is never blindly re-applied (FR-013).
	res, err := a.Update(context.Background(), app.UpdateRequest{Root: root, Names: []string{"alpha"}})
	if err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	s := res.Skills[0]
	if s.Outcome != app.UpdateOutcomeNoChange || s.Reason == "" || s.From != ver110 || s.To != ver110 {
		t.Errorf("no-op = %+v, want no-change at 1.1.0 with reason", s)
	}
	if res.Changed || res.Updated != 0 || res.NoChange != 1 {
		t.Errorf("counts = %+v, want a pure no-op", res)
	}
}

func TestUpdate_UnknownNameFails(t *testing.T) {
	t.Parallel()

	a, root, _, _ := updateProject(t)
	_, err := a.Update(context.Background(), app.UpdateRequest{Root: root, Names: []string{"nope"}})
	if !errors.Is(err, errs.ErrInvalidLock) {
		t.Fatalf("err = %v, want invalid-lock (unknown skill)", err)
	}
}

func TestUpdate_PreservesRequestedIntent(t *testing.T) {
	t.Parallel()

	a, root, _, _ := updateProject(t)
	if _, err := a.Update(context.Background(), app.UpdateRequest{Root: root}); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, name := range []string{"alpha", "beta"} {
		rec := lockRecord(t, root, name)
		if rec.Requested.Version != "^1.0.0" {
			t.Errorf("%s requested = %q, want ^1.0.0 preserved (FR-019)", name, rec.Requested.Version)
		}
		if rec.Resolved.Version != "1.1.0" {
			t.Errorf("%s resolved = %q, want 1.1.0", name, rec.Resolved.Version)
		}
	}
}
