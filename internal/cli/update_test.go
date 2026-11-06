package cli

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
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/testutil"
	"github.com/glapsfun/gskill/internal/tui"
)

// ver110 is the release every update fixture publishes after the add.
const ver110 = "1.1.0"

// withUpdateTableSeams swaps the stdin-TTY probe and the table runner. Each
// table round is served by the next stub in order; a round beyond the last
// stub fails the test.
func withUpdateTableSeams(t *testing.T, tty bool, rounds ...func(tui.UpdateTable) (tui.UpdateTableResult, error)) {
	t.Helper()
	oldTTY, oldRun := stdinIsTTY, runUpdateTableFn
	stdinIsTTY = func() bool { return tty }
	calls := 0
	runUpdateTableFn = func(tbl tui.UpdateTable, _ bool) (tui.UpdateTableResult, error) {
		calls++
		if calls > len(rounds) {
			t.Fatalf("unexpected table round %d", calls)
			return tui.UpdateTableResult{Cancelled: true}, nil
		}
		return rounds[calls-1](tbl)
	}
	t.Cleanup(func() { stdinIsTTY, runUpdateTableFn = oldTTY, oldRun })
}

// tableRow finds the named row in a table round.
func tableRow(t *testing.T, tbl tui.UpdateTable, name string) tui.TableRow {
	t.Helper()
	for _, g := range tbl.Groups {
		for _, r := range g.Rows {
			if r.Name == name {
				return r
			}
		}
	}
	t.Fatalf("table has no row %q: %+v", name, tbl.Groups)
	return tui.TableRow{}
}

// cancelRound is a table round that cancels.
func cancelRound(tui.UpdateTable) (tui.UpdateTableResult, error) {
	return tui.UpdateTableResult{Cancelled: true}, nil
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
	for _, want := range []string{ver110, "1.0.0"} {
		if strings.Contains(string(raw), `"version": "`+want+`"`) {
			return want
		}
	}
	return ""
}

//nolint:paralleltest // swaps package-level table seams
func TestUpdateSelectorEligible_Matrix(t *testing.T) {
	withUpdateTableSeams(t, true)
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
		{"--repo", updateCmd{Repo: []string{"acme/skills"}}, interactive, Globals{}, false},
	}
	for _, tc := range cases {
		if got := tc.cmd.selectorEligible(tc.out, tc.g); got != tc.want {
			t.Errorf("%s: eligible = %v, want %v", tc.name, got, tc.want)
		}
	}

	withUpdateTableSeams(t, false)
	if (updateCmd{}).selectorEligible(interactive, Globals{}) {
		t.Error("non-TTY stdin must not open the table")
	}
}

// checkFirstRound asserts the first table round of the demo plus local-only
// project: one group per repository, every skill present, no banner yet.
func checkFirstRound(t *testing.T, tbl tui.UpdateTable) {
	t.Helper()
	if len(tbl.Groups) != 2 {
		t.Errorf("groups = %d, want 2 (one per repository)", len(tbl.Groups))
	}
	demo := tableRow(t, tbl, "demo")
	if !demo.Eligible || demo.Current != "1.0.0" || demo.Candidate != ver110 || demo.Policy != "^1.0.0" {
		t.Errorf("demo row = %+v, want the computed candidate", demo)
	}
	if local := tableRow(t, tbl, "local-only"); local.Eligible || local.Status != "local source" {
		t.Errorf("local-only row = %+v, want an ineligible local source", local)
	}
	if tbl.Banner != "" {
		t.Errorf("first round has a banner: %q", tbl.Banner)
	}
}

// The table gets every tracked skill, eligible or not, applies exactly the
// confirmed selection, and reopens refreshed with the round's summary (spec
// 032 US1).
//
//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_TableReceivesGroupedPlanAndAppliesSelection(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	if _, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: addSourceTree(t, "local-only"), All: true, Agents: []string{"claude"},
	}); err != nil {
		t.Fatalf("add local: %v", err)
	}

	withUpdateTableSeams(t, true,
		func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
			checkFirstRound(t, tbl)
			return tui.UpdateTableResult{Names: []string{"demo"}, Cursor: "demo"}, nil
		},
		func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
			if demo := tableRow(t, tbl, "demo"); demo.Eligible || demo.Current != ver110 {
				t.Errorf("refreshed demo row = %+v, want up to date at 1.1.0", demo)
			}
			if !strings.Contains(tbl.Banner, "Updated 1 skill") || tbl.Cursor != "demo" {
				t.Errorf("round 2 banner %q cursor %q, want the run summary and demo", tbl.Banner, tbl.Cursor)
			}
			return tui.UpdateTableResult{Cancelled: true}, nil
		},
	)

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := lockedVersion(t, root); got != ver110 {
		t.Errorf("lock version = %q, want confirmed selection applied (1.1.0)", got)
	}
	if !strings.Contains(out.String(), "updated") {
		t.Errorf("stdout missing result row:\n%s", out.String())
	}
}

//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_TableCancelAppliesNothing(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	before := readLock(t, root)
	withUpdateTableSeams(t, true, cancelRound)

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if err != nil {
		t.Fatalf("cancel must exit 0, got %v", err)
	}
	if readLock(t, root) != before {
		t.Error("cancel changed skills-lock.json")
	}
}

//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_TableInterruptExits130(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	before := readLock(t, root)
	withUpdateTableSeams(t, true, func(tui.UpdateTable) (tui.UpdateTableResult, error) {
		return tui.UpdateTableResult{Interrupted: true}, nil
	})

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if errs.ExitCode(err) != int(errs.CodeCancelled) {
		t.Fatalf("interrupt err = %v, want exit %d", err, errs.CodeCancelled)
	}
	if readLock(t, root) != before {
		t.Error("interrupt changed skills-lock.json")
	}
}

//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_InteractiveReportsUncheckedAsFailed(t *testing.T) {
	// The table path must be as loud about discovery failures as the
	// unattended path: unchecked skills are shown as failed, run as failed
	// rows, and the session ends with the partial exit code (spec 032 R6).
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

	withUpdateTableSeams(t, true,
		func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
			if broke := tableRow(t, tbl, "broke"); broke.Eligible || broke.Status != "lookup failed" {
				t.Errorf("broke row = %+v, want an ineligible failed lookup", broke)
			}
			return tui.UpdateTableResult{Names: []string{"demo"}}, nil
		},
		cancelRound,
	)

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if errs.ExitCode(err) != int(errs.CodePartialInstall) {
		t.Errorf("exit = %d (%v), want partial-install parity with the unattended path", errs.ExitCode(err), err)
	}
	if !strings.Contains(out.String(), "failed") || !strings.Contains(out.String(), "broke") {
		t.Errorf("stdout missing the unchecked skill's failed row:\n%s", out.String())
	}
	if got := lockedVersion(t, root); got != ver110 {
		t.Errorf("lock version = %q, want the confirmed selection still applied", got)
	}
}

// With nothing to update the table still opens and shows every skill (spec
// 032 FR-019); quitting it exits 0.
//
//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_NothingActionableStillOpensTable(t *testing.T) {
	a := updateTestApp(t)
	root := agentProject(t)
	src := addSourceTree(t, "local-only")
	if _, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: src, All: true, Agents: []string{"claude"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	withUpdateTableSeams(t, true, func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
		if r := tableRow(t, tbl, "local-only"); r.Eligible || r.Status != "local source" {
			t.Errorf("local-only row = %+v, want an ineligible local source", r)
		}
		return tui.UpdateTableResult{Cancelled: true}, nil
	})

	var out, errb bytes.Buffer
	err := updateCmd{}.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

// The review in the table is the only confirmation (spec 032 FR-013): a
// scripted "no" on stdin must not stop the confirmed selection.
//
//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_NoSecondConfirmation(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	withUpdateTableSeams(t, true,
		func(tui.UpdateTable) (tui.UpdateTableResult, error) {
			return tui.UpdateTableResult{Names: []string{"demo"}}, nil
		},
		cancelRound,
	)

	var out, errb bytes.Buffer
	o := newInteractiveOutput(&out, &errb, "n\n")
	if err := (updateCmd{}).Run(context.Background(), o, a, projectRoot(root), Globals{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(errb.String(), "Continue?") {
		t.Errorf("a second confirmation was asked:\n%s", errb.String())
	}
	if got := lockedVersion(t, root); got != ver110 {
		t.Errorf("lock version = %q, want the reviewed selection applied", got)
	}
}

// An interactive dry run writes nothing, and the next round still offers and
// keeps the same selection (spec 032 US1 scenario 9).
//
//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_DryRunKeepsSelection(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	before := readLock(t, root)
	withUpdateTableSeams(t, true,
		func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
			if !tbl.DryRun {
				t.Error("table was not told about the dry run")
			}
			return tui.UpdateTableResult{Names: []string{"demo"}}, nil
		},
		func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
			if !tableRow(t, tbl, "demo").Eligible || strings.Join(tbl.Selected, ",") != "demo" {
				t.Errorf("round 2 selected %v, demo eligible %v; want demo kept", tbl.Selected, tableRow(t, tbl, "demo").Eligible)
			}
			return tui.UpdateTableResult{Cancelled: true}, nil
		},
	)

	var out, errb bytes.Buffer
	if err := (updateCmd{}).Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{DryRun: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if readLock(t, root) != before {
		t.Error("interactive dry run changed skills-lock.json")
	}
	if !strings.Contains(out.String(), "would update") {
		t.Errorf("stdout missing the would-update result:\n%s", out.String())
	}
}

//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_NoTrackedSkillsSkipsTable(t *testing.T) {
	a := updateTestApp(t)
	root := agentProject(t)
	withUpdateTableSeams(t, true)

	var out, errb bytes.Buffer
	if err := (updateCmd{}).Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), noSkillsDeclared) {
		t.Errorf("stdout = %q, want %q", out.String(), noSkillsDeclared)
	}
}

// readLock returns root's lockfile bytes as a string.
func readLock(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "skills-lock.json")) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// lockedSkillVersion reads one skill's resolved version from root's lock.
func lockedSkillVersion(t *testing.T, root, name string) string {
	t.Helper()
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := l.Entry(name)
	if !ok {
		t.Fatalf("lock has no entry %q", name)
	}
	return skillslock.ToRecord(name, e).Resolved.Version
}

// mixedPolicyProject tracks three skills from one repository under ^1.0.0,
// ~1.0.0, and =1.0.0, then publishes v1.0.1 and v1.1.0.
func mixedPolicyProject(t *testing.T) (*app.App, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	a := updateTestApp(t)
	root := agentProject(t)
	repo := t.TempDir()
	names := []string{"caret", "tilde", "exact"}
	write := func(note string) {
		for _, n := range names {
			updWriteSkill(t, repo, n, "---\nname: "+n+"\ndescription: "+note+"\n---\n# "+n+" "+note+"\n")
		}
	}
	write("v1.0.0")
	updGitRun(t, repo, "init", "--quiet", "-b", "main")
	updGitRun(t, repo, "add", ".")
	updGitRun(t, repo, "commit", "--quiet", "-m", "v1.0.0")
	updGitRun(t, repo, "tag", "v1.0.0")
	for name, version := range map[string]string{"caret": "^1.0.0", "tilde": "~1.0.0", "exact": "=1.0.0"} {
		if _, err := a.Add(context.Background(), app.AddRequest{
			Root: root, Source: repo, Version: version, Selectors: []string{name}, Agents: []string{"claude"},
		}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	for _, tag := range []string{"v1.0.1", "v1.1.0"} {
		write(tag)
		updGitRun(t, repo, "add", ".")
		updGitRun(t, repo, "commit", "--quiet", "-m", tag)
		updGitRun(t, repo, "tag", tag)
	}
	return a, root
}

// A group selection moves each skill within its own policy and leaves the pin
// in place: nothing is forced onto a shared revision (spec 032 US2 scenario 7).
//
//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_GroupWithMixedPoliciesMovesEachWithinItsOwnPolicy(t *testing.T) {
	a, root := mixedPolicyProject(t)
	withUpdateTableSeams(t, true,
		func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
			if len(tbl.Groups) != 1 {
				t.Fatalf("groups = %d, want the one repository", len(tbl.Groups))
			}
			var names []string
			for _, r := range tbl.Groups[0].Rows {
				if r.Eligible {
					names = append(names, r.Name)
				}
			}
			if got := tableRow(t, tbl, "caret").Candidate; got != ver110 {
				t.Errorf("caret candidate = %q, want 1.1.0", got)
			}
			if got := tableRow(t, tbl, "tilde").Candidate; got != "1.0.1" {
				t.Errorf("tilde candidate = %q, want 1.0.1", got)
			}
			if tableRow(t, tbl, "exact").Eligible {
				t.Error("the exact pin is eligible")
			}
			return tui.UpdateTableResult{Names: names}, nil
		},
		cancelRound,
	)

	var out, errb bytes.Buffer
	if err := (updateCmd{}).Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for name, want := range map[string]string{"caret": ver110, "tilde": "1.0.1", "exact": "1.0.0"} {
		if got := lockedSkillVersion(t, root, name); got != want {
			t.Errorf("%s locked at %q, want %q", name, got, want)
		}
	}
}

// repoLabel returns the repository label of the plan's named skill.
func repoLabel(t *testing.T, a *app.App, root, name string) string {
	t.Helper()
	plan, err := a.PlanUpdate(context.Background(), app.UpdatePlanRequest{Root: root})
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	for _, it := range plan.Items {
		if it.Name == name {
			return it.Repo.Label
		}
	}
	t.Fatalf("plan has no %q", name)
	return ""
}

// --repo bypasses the table on an interactive terminal, as skill names do
// (spec 032 FR-022).
//
//nolint:paralleltest // swaps package-level table seams
func TestUpdateRun_RepoOnInteractiveTerminalBypassesTable(t *testing.T) {
	a, root := interactiveUpdateProject(t)
	label := repoLabel(t, a, root, "demo")
	withUpdateTableSeams(t, true)

	var out, errb bytes.Buffer
	if err := (updateCmd{Repo: []string{label}}).Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(root), Globals{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := lockedVersion(t, root); got != ver110 {
		t.Errorf("lock version = %q, want the group updated directly", got)
	}
}

// twoRepoProject tracks alpha and beta from two repositories, both ^1.0.0
// with v1.1.0 published.
func twoRepoProject(t *testing.T) (*app.App, string) {
	t.Helper()
	a := updateTestApp(t)
	root := agentProject(t)
	for _, name := range []string{"alpha", "beta"} {
		repo := updSkillRepo(t, name)
		if _, err := a.Add(context.Background(), app.AddRequest{
			Root: root, Source: repo, Version: "^1.0.0", All: true, Agents: []string{"claude"},
		}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
		updWriteSkill(t, repo, name, "---\nname: "+name+"\ndescription: updated\n---\n# "+name+" v1.1.0\n")
		updGitRun(t, repo, "add", ".")
		updGitRun(t, repo, "commit", "--quiet", "-m", "v1.1.0")
		updGitRun(t, repo, "tag", "v1.1.0")
	}
	return a, root
}

// Selecting a group in the table and naming it with --repo update the same
// skills to the same revisions (spec 032 SC-005, FR-029).
//
//nolint:paralleltest // swaps package-level table seams
func TestUpdate_TableGroupSelectionEqualsRepoFlag(t *testing.T) {
	tableApp, tableRoot := twoRepoProject(t)
	flagApp, flagRoot := twoRepoProject(t)
	alphaGroup := repoLabel(t, tableApp, tableRoot, "alpha")

	withUpdateTableSeams(t, true,
		func(tbl tui.UpdateTable) (tui.UpdateTableResult, error) {
			var names []string
			for _, g := range tbl.Groups {
				if g.Label != alphaGroup {
					continue
				}
				for _, r := range g.Rows {
					if r.Eligible {
						names = append(names, r.Name)
					}
				}
			}
			return tui.UpdateTableResult{Names: names}, nil
		},
		cancelRound,
	)
	var out, errb bytes.Buffer
	if err := (updateCmd{}).Run(context.Background(), interactiveOutput(&out, &errb), tableApp, projectRoot(tableRoot), Globals{}); err != nil {
		t.Fatalf("table run: %v", err)
	}
	piped := NewOutput(&out, &errb, OutputOptions{})
	if err := (updateCmd{Repo: []string{repoLabel(t, flagApp, flagRoot, "alpha")}}).Run(context.Background(), piped, flagApp, projectRoot(flagRoot), Globals{}); err != nil {
		t.Fatalf("--repo run: %v", err)
	}

	for _, name := range []string{"alpha", "beta"} {
		if a, b := lockedSkillVersion(t, tableRoot, name), lockedSkillVersion(t, flagRoot, name); a != b {
			t.Errorf("%s: table locked %q, --repo locked %q", name, a, b)
		}
	}
	if got := lockedSkillVersion(t, flagRoot, "alpha"); got != ver110 {
		t.Errorf("alpha = %q, want 1.1.0", got)
	}
	if got := lockedSkillVersion(t, flagRoot, "beta"); got != "1.0.0" {
		t.Errorf("beta = %q, want untouched 1.0.0", got)
	}
}
