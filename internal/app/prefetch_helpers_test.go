package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/testutil"
)

// gitMultiSkillRepo creates a local git repository holding one committed
// skill per name under a subdirectory each, and returns its path. Mirrors
// gitSkillRepo (progress_test.go) but takes testing.TB so benchmarks can use
// it, and embeds tb.Name() so every test's repo hashes to a unique commit
// (the app tests share one GSKILL_HOME; identical content would warm the
// shared cache across tests and turn cold-fetch assertions into flakes).
func gitMultiSkillRepo(tb testing.TB, name string, skills ...string) string {
	tb.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("git not available")
	}
	repo := filepath.Join(tb.TempDir(), name)
	if err := os.MkdirAll(repo, 0o750); err != nil {
		tb.Fatal(err)
	}
	run := func(args ...string) {
		tb.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "-b", "main")
	for _, s := range skills {
		dir := filepath.Join(repo, s)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			tb.Fatal(err)
		}
		body := "---\nname: " + s + "\ndescription: a skill\n---\n# " + s + "\n\ntest: " + tb.Name() + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			tb.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "initial")
	return repo
}

// projectWithAgentTB is projectWithAgent for benchmarks: an initialized
// project with a .claude marker so an agent is detected.
func projectWithAgentTB(tb testing.TB) string {
	tb.Helper()
	root := tb.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		tb.Fatal(err)
	}
	a := app.New(app.Options{Agents: agent.NewDefaultRegistry(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := a.Init(context.Background(), root, false); err != nil {
		tb.Fatal(err)
	}
	return root
}

// stripGskillExt rewrites the project's skills-lock.json without any entry's
// gskill extension block and removes all local install state, simulating a
// fresh clone of a lock written by a foreign tool: no pins, no store, no
// targets — exactly the slow path this feature optimizes.
func stripGskillExt(tb testing.TB, root string) {
	tb.Helper()

	lockPath := filepath.Join(root, skillslock.FileName)
	data, err := os.ReadFile(lockPath) //nolint:gosec // test-controlled temp path
	if err != nil {
		tb.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		tb.Fatal(err)
	}
	skills, ok := doc["skills"].(map[string]any)
	if !ok {
		tb.Fatalf("skills-lock.json has no skills object")
	}
	for _, v := range skills {
		if entry, ok := v.(map[string]any); ok {
			delete(entry, "gskill")
		}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(lockPath, out, 0o600); err != nil {
		tb.Fatal(err)
	}
	for _, d := range []string{".gskill", filepath.Join(".claude", "skills"), ".agents"} {
		if err := os.RemoveAll(filepath.Join(root, d)); err != nil {
			tb.Fatal(err)
		}
	}
}

// countingGitApp builds an App with the counting runner injected and a cold,
// test-private GskillHome so the shared cache/store never satisfies a fetch.
// (countingApp is taken: onboard_test.go's counts agent detections.)
func countingGitApp(tb testing.TB, c *testutil.CountingGit) *app.App {
	tb.Helper()

	return app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Git:        c,
		GskillHome: tb.TempDir(),
	})
}
