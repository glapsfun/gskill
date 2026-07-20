package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/tui"
)

// Spec 011 T014: the `add` interactive branch — wizard session pre-fill from
// flags (FR-004), the all-answers collapse, FR-001's single-skill wizard, and
// cancel → exit 130 with zero writes.

// interactiveOutput builds an Output that reports interactive despite buffer
// streams, so tests can exercise the wizard branch in-process.
func interactiveOutput(stdout, stderr *bytes.Buffer) *Output {
	return &Output{stdout: stdout, stderr: stderr, interactive: true}
}

// withWizardSeams swaps the stdin-TTY probe and the wizard runner for a test.
func withWizardSeams(t *testing.T, tty bool, run func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error)) {
	t.Helper()

	oldTTY, oldRun := stdinIsTTY, runWizardFn
	stdinIsTTY = func() bool { return tty }
	if run != nil {
		runWizardFn = run
	}
	t.Cleanup(func() { stdinIsTTY, runWizardFn = oldTTY, oldRun })
}

func TestAddWizardSession_PrefillFromFlags(t *testing.T) {
	t.Parallel()

	c := addCmd{Source: "example/repo", Version: "^1.0.0", Agent: []string{"claude", "codex"}}
	s := c.wizardSession(true)

	if !s.SourceAnswered || s.Source != "example/repo" {
		t.Errorf("source not pre-filled: %+v", s)
	}
	if !s.VersionAnswered || s.Version != "^1.0.0" {
		t.Errorf("--version must answer the version step: %+v", s)
	}
	if !s.AgentsAnswered || len(s.AgentIDs) != 2 {
		t.Errorf("--agent must answer the agents step: %+v", s)
	}
	if !s.ApprovalAnswered {
		t.Error("--yes must answer the approval step")
	}

	bare := addCmd{Source: "example/repo"}
	sb := bare.wizardSession(false)
	if sb.VersionAnswered || sb.AgentsAnswered || sb.ApprovalAnswered || sb.SkillsAnswered {
		t.Errorf("bare add must leave all question steps unanswered: %+v", sb)
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_WizardBranchLaunchesForSingleSkillSource(t *testing.T) {
	// Not parallel: swaps package seams.
	src := addSourceTree(t, "alpha") // single skill: wizard must still open (FR-001)
	dir := agentProject(t)

	var gotCfg *tui.WizardConfig
	withWizardSeams(t, true, func(_ context.Context, cfg tui.WizardConfig, _ bool) (tui.WizardOutcome, error) {
		gotCfg = &cfg
		return tui.WizardOutcome{Cancelled: true}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src}
	err := c.Run(context.Background(), interactiveOutput(&out, &errb), newTestApp(), projectRoot(dir), Globals{})
	if gotCfg == nil {
		t.Fatal("wizard was not launched on an interactive add (FR-001)")
	}
	if !errors.Is(err, errs.ErrCancelled) {
		t.Errorf("cancel outcome error = %v, want errs.ErrCancelled", err)
	}
	if errs.ExitCode(err) != 130 {
		t.Errorf("cancel exit code = %d, want 130", errs.ExitCode(err))
	}
	if gotCfg.Phases.Discover == nil || gotCfg.Phases.Plan == nil || gotCfg.Phases.Execute == nil {
		t.Error("wizard config missing phase closures")
	}
	if gotCfg.Phases.ResolveSelection != nil {
		t.Error("ResolveSelection must be nil when no --skill/--all selectors are given")
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_FlagsPrefillWizardSelection(t *testing.T) {
	src := addSourceTree(t, "alpha", "beta")
	dir := agentProject(t)

	var cfg tui.WizardConfig
	withWizardSeams(t, true, func(_ context.Context, c tui.WizardConfig, _ bool) (tui.WizardOutcome, error) {
		cfg = c
		return tui.WizardOutcome{Cancelled: true}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src, Skill: []string{"alpha"}}
	_ = c.Run(context.Background(), interactiveOutput(&out, &errb), newTestApp(), projectRoot(dir), Globals{})

	if cfg.Phases.ResolveSelection == nil {
		t.Fatal("--skill must install a ResolveSelection closure (FR-004)")
	}
	disc, err := newTestApp().DiscoverSource(context.Background(), app.DiscoverRequest{Root: dir, Source: src})
	if err != nil {
		t.Fatalf("DiscoverSource: %v", err)
	}
	selected, err := cfg.Phases.ResolveSelection(context.Background(), disc)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	if len(selected) != 1 || selected[0].ID != "alpha" {
		t.Errorf("ResolveSelection = %+v, want alpha", selected)
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_AllAnswersCollapseToDirectInstall(t *testing.T) {
	src := addSourceTree(t, "alpha", "beta")
	dir := agentProject(t)

	wizardCalled := false
	withWizardSeams(t, true, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		wizardCalled = true
		return tui.WizardOutcome{}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src, Skill: []string{"alpha"}, Agent: []string{"claude"}}
	err := c.Run(context.Background(), interactiveOutput(&out, &errb), newTestApp(), projectRoot(dir), Globals{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errb.String())
	}
	if wizardCalled {
		t.Error("wizard launched although every step was answered by flags")
	}
	if !strings.Contains(out.String(), "Installed 1 skill(s): alpha") {
		t.Errorf("stdout = %q, want direct-install output", out.String())
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_WizardSuccessPrintsSummaryToStdout(t *testing.T) {
	src := addSourceTree(t, "alpha")
	dir := agentProject(t)

	withWizardSeams(t, true, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		return tui.WizardOutcome{
			Executed: true,
			Result:   app.AddResult{Installed: []app.InstalledSkill{{Name: "alpha"}}},
		}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src}
	if err := c.Run(context.Background(), interactiveOutput(&out, &errb), newTestApp(), projectRoot(dir), Globals{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Installed 1 skill(s): alpha") {
		t.Errorf("stdout = %q, want post-wizard summary line", out.String())
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_NoStdinTTYKeepsDirectPath(t *testing.T) {
	src := addSourceTree(t, "alpha")
	dir := agentProject(t)

	wizardCalled := false
	withWizardSeams(t, false, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		wizardCalled = true
		return tui.WizardOutcome{}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src}
	if err := c.Run(context.Background(), interactiveOutput(&out, &errb), newTestApp(), projectRoot(dir), Globals{}); err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errb.String())
	}
	if wizardCalled {
		t.Error("wizard launched without a stdin TTY")
	}
	if !strings.Contains(out.String(), "Installed 1 skill(s): alpha") {
		t.Errorf("stdout = %q, want direct single-skill install", out.String())
	}
}

// TestAddCmd_FreshDirectoryAutoInitializes (spec 017 FR-001/SC-001): `add`
// succeeds in a directory that has never run `gskill init` — no .gskill,
// .agents, or .gitignore exist beforehand — creating all of them as a side
// effect instead of requiring a separate setup step.
func TestAddCmd_FreshDirectoryAutoInitializes(t *testing.T) {
	t.Parallel()

	src := addSourceTree(t, "solo")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	assertNoLocalProjectState(t, dir)

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "add", src)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %q", code, stderr)
	}
	if !strings.Contains(stdout, "solo") {
		t.Errorf("stdout = %q, want it to mention the installed skill", stdout)
	}

	assertLocalProjectStateCreated(t, dir)

	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "solo", "SKILL.md")); err != nil {
		t.Errorf("skill not installed: %v", err)
	}
}

// ---- FR-024: add --dry-run renders the plan without executing ------------------

func TestAddDryRun_RendersPlanWritesNothing(t *testing.T) {
	t.Parallel()

	src := addSourceTree(t, "alpha", "beta")
	dir := agentProject(t)

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "--dry-run", "add", src, "--all")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %q", code, stderr)
	}
	for _, want := range []string{"alpha", "beta", ".claude"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Installed") {
		t.Errorf("dry-run must not claim an install happened:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills-lock.json")); err == nil {
		t.Error("dry-run wrote a lockfile")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "alpha")); err == nil {
		t.Error("dry-run installed files")
	}
}

func TestAddDryRun_JSONPlan(t *testing.T) {
	t.Parallel()

	src := addSourceTree(t, "alpha")
	dir := agentProject(t)

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "--json", "--dry-run", "add", src)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %q", code, stderr)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if _, ok := v["actions"]; !ok {
		t.Errorf("JSON plan missing actions: %s", stdout)
	}
}

// ---- Review fixes, Phase A -----------------------------------------------------

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_JSONOnTTYKeepsDirectPath(t *testing.T) {
	src := addSourceTree(t, "alpha")
	dir := agentProject(t)

	wizardCalled := false
	withWizardSeams(t, true, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		wizardCalled = true
		return tui.WizardOutcome{}, nil
	})

	var out, errb bytes.Buffer
	o := &Output{stdout: &out, stderr: &errb, interactive: true, json: true}
	c := addCmd{Source: src}
	if err := c.Run(context.Background(), o, newTestApp(), projectRoot(dir), Globals{}); err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errb.String())
	}
	if wizardCalled {
		t.Error("--json on a TTY launched the wizard; machine output must stay direct (contract)")
	}
	var v any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Errorf("stdout is not clean JSON: %v\n%q", err, out.String())
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_LocalAgentAddSkipsWizard(t *testing.T) {
	src := addSourceTree(t, "alpha")
	dir := agentProject(t)
	a := newTestApp()

	// Seed the install non-interactively, then add a second agent on a TTY:
	// this must take the zero-network local-relink fast path, not the wizard.
	if _, _, code := runCLI(t, a, "-C", dir, "add", src); code != 0 {
		t.Fatal("seed add failed")
	}

	wizardCalled := false
	withWizardSeams(t, true, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		wizardCalled = true
		return tui.WizardOutcome{Cancelled: true}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src, Agent: []string{"codex"}}
	if err := c.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(dir), Globals{}); err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errb.String())
	}
	if wizardCalled {
		t.Error("pure agent-add launched the wizard instead of the local relink fast path")
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "skills", "alpha")); err != nil {
		t.Errorf("codex target missing after local agent-add: %v", err)
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_WizardWarningsReachStderr(t *testing.T) {
	src := addSourceTree(t, "alpha")
	dir := agentProject(t)

	withWizardSeams(t, true, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		return tui.WizardOutcome{
			Executed: true,
			Result: app.AddResult{
				Installed: []app.InstalledSkill{{Name: "alpha"}},
				Warnings:  []string{"symlink fell back to copy"},
			},
		}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src}
	if err := c.Run(context.Background(), interactiveOutput(&out, &errb), newTestApp(), projectRoot(dir), Globals{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(errb.String(), "warning: symlink fell back to copy") {
		t.Errorf("wizard warnings lost: stderr = %q", errb.String())
	}
}

// ---- Review round 2, Phase 3 ------------------------------------------------------

//nolint:paralleltest // swaps package-level wizard seams
func TestAddRun_GlobalAgentAddStillOpensWizard(t *testing.T) {
	src := addSourceTree(t, "alpha")
	dir := agentProject(t)
	a := newTestApp()
	if _, _, code := runCLI(t, a, "-C", dir, "add", src); code != 0 {
		t.Fatal("seed add failed")
	}

	wizardCalled := false
	withWizardSeams(t, true, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		wizardCalled = true
		return tui.WizardOutcome{Cancelled: true}, nil
	})

	var out, errb bytes.Buffer
	c := addCmd{Source: src, Agent: []string{"codex"}, Global: true}
	_ = c.Run(context.Background(), interactiveOutput(&out, &errb), a, projectRoot(dir), Globals{})
	if !wizardCalled {
		t.Error("--global agent-add was routed to the locked-scope fast path instead of the wizard")
	}
}
