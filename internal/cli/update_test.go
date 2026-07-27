package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/testutil"
	"github.com/glapsfun/gskill/internal/tui"
)

// withUpdateSelectorSeams swaps the stdin-TTY probe and the selector runner.
func withUpdateSelectorSeams(t *testing.T, tty bool, run func([]tui.UpdateItem, bool) (tui.UpdateSelection, error)) {
	t.Helper()
	oldTTY, oldRun := stdinIsTTY, runUpdateSelectorFn
	stdinIsTTY = func() bool { return tty }
	if run != nil {
		runUpdateSelectorFn = run
	}
	t.Cleanup(func() { stdinIsTTY, runUpdateSelectorFn = oldTTY, oldRun })
}

// updateTestApp builds an App with a private home for update-flow tests.
func updateTestApp(t *testing.T) *app.App {
	t.Helper()
	return app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: filepath.Join(t.TempDir(), "home"),
	})
}

// updGitRun runs a git command in repo.
func updGitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = repo
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// updWriteSkill writes name's SKILL.md into repo (folder identity).
func updWriteSkill(t *testing.T, repo, name, body string) {
	t.Helper()
	dir := filepath.Join(repo, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// updSkillRepo builds a git repo with one skill tagged v1.0.0.
func updSkillRepo(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	updWriteSkill(t, repo, name, "---\nname: "+name+"\ndescription: a skill\n---\n# "+name+" ("+t.Name()+")\n")
	updGitRun(t, repo, "init", "--quiet", "-b", "main")
	updGitRun(t, repo, "add", ".")
	updGitRun(t, repo, "commit", "--quiet", "-m", "initial")
	updGitRun(t, repo, "tag", "v1.0.0")
	return repo
}

// interactiveUpdateProject returns an app and project holding one actionable
// skill named "demo": locked to 1.0.0, with 1.1.0 published after the add.
func interactiveUpdateProject(t *testing.T) (*app.App, string) {
	t.Helper()
	a := updateTestApp(t)
	root := agentProject(t)
	repo := updSkillRepo(t, "demo")
	if _, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: repo, Version: "^1.0.0", All: true, Agents: []string{"claude"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	updWriteSkill(t, repo, "demo", "---\nname: demo\ndescription: updated\n---\n# demo v1.1.0\n")
	updGitRun(t, repo, "add", ".")
	updGitRun(t, repo, "commit", "--quiet", "-m", "v1.1.0")
	updGitRun(t, repo, "tag", "v1.1.0")
	return a, root
}

func lockedVersion(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "skills-lock.json")) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1.1.0", "1.0.0"} {
		if strings.Contains(string(raw), `"version": "`+want+`"`) {
			return want
		}
	}
	return ""
}

//nolint:paralleltest // swaps package-level selector seams
func TestUpdateSelectorEligible_Matrix(t *testing.T) {
	withUpdateSelectorSeams(t, true, nil)
	var out, errb bytes.Buffer
	interactive := interactiveOutput(&out, &errb)
	piped := NewOutput(&out, &errb, OutputOptions{})
	jsonOut := &Output{stdout: &out, stderr: &errb, interactive: true, json: true}

	cases := []struct {
		name string
		cmd  updateCmd
		out  *Output
		g    Globals
		want bool
	}{
		{"interactive terminal", updateCmd{}, interactive, Globals{}, true},
		{"dry-run stays interactive", updateCmd{}, interactive, Globals{DryRun: true}, true},
		{"piped stdout", updateCmd{}, piped, Globals{}, false},
		{"json mode", updateCmd{}, jsonOut, Globals{}, false},
		{"named skills", updateCmd{Skills: []string{"demo"}}, interactive, Globals{}, false},
		{"--yes auto-confirm", updateCmd{}, interactive, Globals{Yes: true}, false},
		{"--list", updateCmd{List: true}, interactive, Globals{}, false},
	}
	for _, tc := range cases {
		if got := tc.cmd.selectorEligible(tc.out, tc.g); got != tc.want {
			t.Errorf("%s: eligible = %v, want %v", tc.name, got, tc.want)
		}
	}

	withUpdateSelectorSeams(t, false, nil)
	if (updateCmd{}).selectorEligible(interactive, Globals{}) {
		t.Error("non-TTY stdin must not open the selector")
	}
}

//nolint:paralleltest // swaps package-level selector seams
func TestUpdateRun_SelectorReceivesActionableAndAppliesSelection(t *testing.T) {
	a, root := interactiveUpdateProject(t)

	var gotItems []tui.UpdateItem
	withUpdateSelectorSeams(t, true, func(items []tui.UpdateItem, _ bool) (tui.UpdateSelection, error) {
		gotItems = items
		return tui.UpdateSelection{Names: []string{"demo"}}, nil
	})

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(gotItems) != 1 || gotItems[0].Name != "demo" ||
		gotItems[0].Current != "1.0.0" || gotItems[0].Candidate != "1.1.0" || gotItems[0].Policy != "^1.0.0" {
		t.Errorf("selector items = %+v, want the computed actionable candidate", gotItems)
	}
	if got := lockedVersion(t, root); got != "1.1.0" {
		t.Errorf("lock version = %q, want confirmed selection applied (1.1.0)", got)
	}
	if !strings.Contains(out.String(), "updated") {
		t.Errorf("stdout missing result row:\n%s", out.String())
	}
}

//nolint:paralleltest // swaps package-level selector seams
func TestUpdateRun_SelectorCancelAppliesNothing(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	withUpdateSelectorSeams(t, true, func([]tui.UpdateItem, bool) (tui.UpdateSelection, error) {
		return tui.UpdateSelection{Cancelled: true}, nil
	})

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if err != nil {
		t.Fatalf("cancel must exit 0, got %v", err)
	}
	if got := lockedVersion(t, root); got != "1.0.0" {
		t.Errorf("lock version = %q, want unchanged 1.0.0 after cancel", got)
	}
}

//nolint:paralleltest // swaps package-level selector seams
func TestUpdateRun_SelectorInterruptExits130(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	withUpdateSelectorSeams(t, true, func([]tui.UpdateItem, bool) (tui.UpdateSelection, error) {
		return tui.UpdateSelection{Interrupted: true}, nil
	})

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if !errors.Is(err, errs.ErrCancelled) && errs.ExitCode(err) != int(errs.CodeCancelled) {
		t.Fatalf("interrupt err = %v, want exit %d", err, errs.CodeCancelled)
	}
	if got := lockedVersion(t, root); got != "1.0.0" {
		t.Errorf("lock version = %q, want unchanged 1.0.0 after interrupt", got)
	}
}

//nolint:paralleltest // swaps package-level selector seams
func TestUpdateRun_InteractiveReportsUncheckedAsFailed(t *testing.T) {
	// Regression (review round 2): the interactive path must be as loud
	// about discovery failures as the unattended path — unchecked skills
	// surface as failed rows with the partial exit code instead of being
	// silently dropped from the selector and the summary.
	a, root := interactiveUpdateProject(t)
	broken := updSkillRepo(t, "broke")
	if _, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: broken, Version: "^1.0.0", All: true, Agents: []string{"claude"},
	}); err != nil {
		t.Fatalf("add broken: %v", err)
	}
	if err := os.RemoveAll(broken); err != nil {
		t.Fatal(err)
	}

	var gotItems []tui.UpdateItem
	withUpdateSelectorSeams(t, true, func(items []tui.UpdateItem, _ bool) (tui.UpdateSelection, error) {
		gotItems = items
		return tui.UpdateSelection{Names: []string{"demo"}}, nil
	})

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if len(gotItems) != 1 || gotItems[0].Name != "demo" {
		t.Errorf("selector items = %+v, want only the checkable candidate", gotItems)
	}
	if errs.ExitCode(err) != int(errs.CodePartialInstall) {
		t.Errorf("exit = %d (%v), want partial-install parity with the unattended path", errs.ExitCode(err), err)
	}
	if !strings.Contains(out.String(), "failed") || !strings.Contains(out.String(), "broke") {
		t.Errorf("stdout missing the unchecked skill's failed row:\n%s", out.String())
	}
	if got := lockedVersion(t, root); got != "1.1.0" {
		t.Errorf("lock version = %q, want the confirmed selection still applied", got)
	}
}

//nolint:paralleltest // swaps package-level selector seams
func TestUpdateRun_NothingActionableSkipsSelector(t *testing.T) {
	a := updateTestApp(t)
	root := agentProject(t)
	src := addSourceTree(t, "local-only")
	if _, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: src, All: true, Agents: []string{"claude"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	withUpdateSelectorSeams(t, true, func([]tui.UpdateItem, bool) (tui.UpdateSelection, error) {
		t.Fatal("selector opened with nothing actionable")
		return tui.UpdateSelection{}, nil
	})

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("stdout missing up-to-date terminal state:\n%s", out.String())
	}
}
