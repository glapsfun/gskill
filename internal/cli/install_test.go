package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/tui"
)

// lockOnlyProject builds a directory containing only a skills-lock.json whose
// two entries point at a local source tree (offline-friendly).
func lockOnlyProject(t *testing.T) (dir string) {
	t.Helper()
	return lockOnlyProjectFromSource(t, addSourceTree(t, "alpha", "beta"))
}

// lockOnlyProjectFromSource is lockOnlyProject, but against a caller-supplied
// source tree — so two independent project directories can share identical
// entries (source, computedHash) for cross-path parity comparisons.
func lockOnlyProjectFromSource(t *testing.T, src string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	entry := func(name string) string {
		hash, err := integrity.CompatHash(filepath.Join(src, "skills", name))
		if err != nil {
			t.Fatal(err)
		}
		return `    "` + name + `": {
      "source": "` + strings.ReplaceAll(src, `\`, `\\`) + `",
      "sourceType": "local",
      "skillPath": "skills/` + name + `/SKILL.md",
      "computedHash": "` + hash + `"
    }`
	}
	lock := "{\n  \"version\": 1,\n  \"skills\": {\n" + entry("alpha") + ",\n" + entry("beta") + "\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func agentDirsExist(t *testing.T, dir string, agents ...string) {
	t.Helper()
	for _, ag := range agents {
		for _, skill := range []string{"alpha", "beta"} {
			if _, err := os.Stat(filepath.Join(dir, "."+ag, "skills", skill)); err != nil {
				t.Errorf("target %s/%s missing: %v", ag, skill, err)
			}
		}
	}
}

// TestInstallCmd_RequestAgentsNilWhenFlagAbsent (research.md Decision 6): the
// nil-vs-explicit-empty distinction that FR-012/FR-017 depend on starts at
// the CLI boundary — Kong must leave Agent nil (not an empty slice) when
// --agent is not passed, since app.InstallFromLockRequest.Agents == nil is
// how "no explicit selection" (FR-002a) is told apart from an explicit empty
// selection (FR-012, TUI-only).
func TestInstallCmd_RequestAgentsNilWhenFlagAbsent(t *testing.T) {
	t.Parallel()
	var c installCmd
	req := c.request("/tmp/does-not-matter", Globals{})
	if req.Agents != nil {
		t.Fatalf("Agents = %#v, want nil when --agent is not passed", req.Agents)
	}
}

// TestInstall_AgentFlagForms (T024/FR-012): comma-separated and repeated
// --agent produce identical results.
func TestInstall_AgentFlagForms(t *testing.T) {
	t.Parallel()

	comma := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", comma, "install", "--agent", "claude,codex"); code != 0 {
		t.Fatalf("comma form: code %d, stderr %q", code, stderr)
	}
	agentDirsExist(t, comma, "claude", "codex")

	repeated := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", repeated, "install", "--agent", "claude", "--agent", "codex"); code != 0 {
		t.Fatalf("repeated form: code %d, stderr %q", code, stderr)
	}
	agentDirsExist(t, repeated, "claude", "codex")
}

// TestInstall_FlagConflicts (T024): incompatible or invalid flags exit 2.
func TestInstall_FlagConflicts(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)

	if _, _, code := runCLI(t, newTestApp(), "-C", dir, "install", "--force", "--frozen-lockfile"); code != 2 {
		t.Errorf("--force --frozen-lockfile: code = %d, want 2", code)
	}
	if _, _, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--install-mode", "bogus"); code != 2 {
		t.Errorf("--install-mode bogus: code = %d, want 2", code)
	}
}

// TestInstall_CopyAliasRecordsMode (T024): --copy is a deprecated alias for
// --install-mode copy and lands in the recorded metadata.
func TestInstall_CopyAliasRecordsMode(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--copy"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	l, err := skillslock.Load(filepath.Join(dir, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := l.Entry("alpha")
	if !ok || e.Ext == nil {
		t.Fatalf("alpha entry/ext missing")
	}
	if e.Ext.InstallMode != "copy" {
		t.Errorf("installMode = %q, want copy", e.Ext.InstallMode)
	}
}

// TestInstall_NoInitRefuses (T024/FR-019): --no-init on an uninitialized
// project fails instead of scaffolding.
// TestInstall_NoInitRefuses also covers spec 017 FR-003/SC-004: refusing must
// leave zero partial local project state behind, not just exit non-zero.
func TestInstall_NoInitRefuses(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--no-init")
	if code == 0 {
		t.Fatal("want failure with --no-init on an uninitialized project")
	}
	if !strings.Contains(stderr, "no-init") && !strings.Contains(stderr, "not initialized") {
		t.Errorf("stderr %q should explain the refusal", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill.toml")); err == nil {
		t.Error("gskill.toml was created despite --no-init")
	}
	for _, notCreated := range []string{".gskill", ".agents", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, notCreated)); !os.IsNotExist(err) {
			t.Errorf("%s was created despite --no-init refusing to initialize (no partial state allowed)", notCreated)
		}
	}
}

// TestInstall_FreshLockOnlyProjectAutoInitializes (spec 017 FR-002/SC-001):
// lockOnlyProject creates only skills-lock.json — no .gskill, .agents, or
// .gitignore. A plain `install` (no --no-init) must still succeed, creating
// the missing local project state as a side effect.
func TestInstall_FreshLockOnlyProjectAutoInitializes(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	assertNoLocalProjectState(t, dir)

	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %q", code, stderr)
	}

	assertLocalProjectStateCreated(t, dir)
	agentDirsExist(t, dir, "claude")
}

// TestInstall_DryRunWritesNothing (T026/FR-015): the plan is reported and the
// tree is untouched.
func TestInstall_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	before, err := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--dry-run")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "alpha") {
		t.Errorf("dry-run output should list the plan:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill.toml")); err == nil {
		t.Error("dry-run created gskill.toml")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); err == nil {
		t.Error("dry-run activated agent targets")
	}
	after, _ := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if string(before) != string(after) {
		t.Error("dry-run modified the lock")
	}
}

// TestInstall_AgentNarrowsAndReportsDiff (spec 013 FR-001/FR-014/FR-019): an
// explicit --agent selection narrows the project and the CLI reports the
// exact narrowed top-level agent set plus per-skill kept/added/removed.
func TestInstall_AgentNarrowsAndReportsDiff(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude,codex,cursor"); code != 0 {
		t.Fatalf("baseline install: code %d, stderr %q", code, stderr)
	}
	agentDirsExist(t, dir, "claude", "codex", "cursor")

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--json")
	if code != 0 {
		t.Fatalf("narrow install: code %d, stderr %q", code, stderr)
	}
	var doc struct {
		Agents []string `json:"agents"`
		Skills []struct {
			Name          string   `json:"name"`
			AgentsKept    []string `json:"agentsKept"`
			AgentsAdded   []string `json:"agentsAdded"`
			AgentsRemoved []string `json:"agentsRemoved"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, stdout)
	}
	if len(doc.Agents) != 1 || doc.Agents[0] != "claude" {
		t.Errorf("top-level agents = %v, want [claude] (not the pre-narrowing union)", doc.Agents)
	}
	for _, s := range doc.Skills {
		if len(s.AgentsKept) != 1 || s.AgentsKept[0] != "claude" {
			t.Errorf("%s agentsKept = %v, want [claude]", s.Name, s.AgentsKept)
		}
		if s.AgentsAdded == nil || len(s.AgentsAdded) != 0 {
			t.Errorf("%s agentsAdded = %v, want [] (present, not omitted)", s.Name, s.AgentsAdded)
		}
		if len(s.AgentsRemoved) != 2 {
			t.Errorf("%s agentsRemoved = %v, want [codex cursor]", s.Name, s.AgentsRemoved)
		}
	}
	assertAgentTargetsRemoved(t, dir, []string{".codex", ".cursor"}, []string{"alpha", "beta"})
}

// assertAgentTargetsRemoved checks that none of the given agent markers has
// a target for any of the given skills.
func assertAgentTargetsRemoved(t *testing.T, dir string, markers, skills []string) {
	t.Helper()
	for _, marker := range markers {
		for _, skill := range skills {
			if _, err := os.Stat(filepath.Join(dir, marker, "skills", skill)); err == nil {
				t.Errorf("%s/%s not removed after narrowing", marker, skill)
			}
		}
	}
}

// TestInstall_DryRunNarrowShowsAgentPlan (contracts/cli-install-agent-
// replace.md "--dry-run output"): a narrowing dry-run reports the
// keep/remove/lock plan without writing anything.
func TestInstall_DryRunNarrowShowsAgentPlan(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude,codex"); code != 0 {
		t.Fatalf("baseline install: code %d, stderr %q", code, stderr)
	}
	before, err := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--dry-run")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"keep:", "remove:", "lock:", "codex"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("dry-run diagnostics missing %q:\n%s", want, stderr)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if string(before) != string(after) {
		t.Error("dry-run narrowing modified the lock")
	}
	agentDirsExist(t, dir, "claude", "codex") // nothing actually removed
}

// TestInstall_FrozenAgentMismatchErrorFormat (contracts/cli-install-agent-
// replace.md "--frozen-lockfile interaction"): the error names the skill and
// both the locked and requested agent sets.
func TestInstall_FrozenAgentMismatchErrorFormat(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude,codex"); code != 0 {
		t.Fatalf("baseline install: code %d, stderr %q", code, stderr)
	}

	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--frozen-lockfile")
	if code != 4 {
		t.Fatalf("code = %d, want 4 (CodeLockMismatch)", code)
	}
	for _, want := range []string{"locked:", "requested:", "claude", "codex"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

// TestInstall_CLIAndWizardExecuteProduceIdenticalOutcome (spec 013 SC-005):
// the CLI direct path and the wizard's Execute callback both funnel through
// the same app.InstallFromLock construction (research.md Decision 5) — this
// proves it end-to-end from two identical starting fixtures, not just by
// code inspection. The "TUI path" here is the exact request construction
// runLockWizard's Execute closure uses, bypassing only terminal rendering.
func TestInstall_CLIAndWizardExecuteProduceIdenticalOutcome(t *testing.T) {
	t.Parallel()
	src := addSourceTree(t, "alpha", "beta")
	dirCLI := lockOnlyProjectFromSource(t, src)
	dirWizard := lockOnlyProjectFromSource(t, src)

	for _, dir := range []string{dirCLI, dirWizard} {
		if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude,codex,cursor"); code != 0 {
			t.Fatalf("baseline install (%s): code %d, stderr %q", dir, code, stderr)
		}
	}

	if _, stderr, code := runCLI(t, newTestApp(), "-C", dirCLI, "install", "--agent", "claude"); code != 0 {
		t.Fatalf("CLI narrow: code %d, stderr %q", code, stderr)
	}

	c := installCmd{}
	req := c.request(dirWizard, Globals{})
	req.Agents = []string{"claude"}
	if _, err := newTestApp().InstallFromLock(context.Background(), req); err != nil {
		t.Fatalf("wizard-equivalent narrow: %v", err)
	}

	assertIdenticalNarrowOutcome(t, dirCLI, dirWizard, []string{"claude"}, []string{".codex", ".cursor"})
}

// TestInstall_CLIAndWizardExecuteAgreeOnZeroAgents (spec 013 SC-005/FR-012):
// the same parity check for the zero-agent narrowing case, reachable in
// production only through the TUI.
func TestInstall_CLIAndWizardExecuteAgreeOnZeroAgents(t *testing.T) {
	t.Parallel()
	src := addSourceTree(t, "alpha", "beta")
	dirA := lockOnlyProjectFromSource(t, src)
	dirB := lockOnlyProjectFromSource(t, src)

	for _, dir := range []string{dirA, dirB} {
		if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude"); code != 0 {
			t.Fatalf("baseline install (%s): code %d, stderr %q", dir, code, stderr)
		}
	}

	// Path A: InstallFromLock called directly with an explicit empty set.
	if _, err := newTestApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: dirA, Agents: []string{},
	}); err != nil {
		t.Fatalf("direct zero-agent narrow: %v", err)
	}
	// Path B: the wizard's Execute-callback construction with a confirmed
	// zero-length (non-nil) selection.
	c := installCmd{}
	req := c.request(dirB, Globals{})
	req.Agents = []string{}
	if _, err := newTestApp().InstallFromLock(context.Background(), req); err != nil {
		t.Fatalf("wizard-equivalent zero-agent narrow: %v", err)
	}

	assertIdenticalNarrowOutcome(t, dirA, dirB, nil, []string{"." + "claude"})
}

// assertIdenticalNarrowOutcome compares two projects' lock agent lists and
// on-disk agent directories after independently applying what should be an
// equivalent narrowing.
func assertIdenticalNarrowOutcome(t *testing.T, dirA, dirB string, wantAgents, removedMarkers []string) {
	t.Helper()
	assertSameLockedAgents(t, dirA, dirB, wantAgents)
	assertAgentTargetsRemoved(t, dirA, removedMarkers, []string{"alpha", "beta"})
	assertAgentTargetsRemoved(t, dirB, removedMarkers, []string{"alpha", "beta"})
}

// assertSameLockedAgents checks that dirA's and dirB's lock entries record
// the same (non-nil-normalized) agent set, and that it equals want.
func assertSameLockedAgents(t *testing.T, dirA, dirB string, want []string) {
	t.Helper()
	lockA, err := skillslock.Load(filepath.Join(dirA, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lockB, err := skillslock.Load(filepath.Join(dirB, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	wantNorm := append([]string{}, want...)
	for _, name := range []string{"alpha", "beta"} {
		eA, _ := lockA.Entry(name)
		eB, _ := lockB.Entry(name)
		if eA.Ext == nil || eB.Ext == nil {
			t.Fatalf("%s: missing gskill metadata (A=%v B=%v)", name, eA.Ext, eB.Ext)
		}
		gotA := append([]string{}, eA.Ext.Agents...)
		gotB := append([]string{}, eB.Ext.Agents...)
		if !reflect.DeepEqual(gotA, gotB) {
			t.Errorf("%s agents diverge: dirA=%v dirB=%v", name, gotA, gotB)
		}
		if !reflect.DeepEqual(gotA, wantNorm) {
			t.Errorf("%s agents = %v, want %v", name, gotA, wantNorm)
		}
	}
}

// TestInstall_AgentEqualsEmptyIsUsageError (code-review fix): `--agent=`
// parses via Kong into a non-nil, empty []string — indistinguishable at the
// app layer from a deliberate TUI zero-agent narrowing (FR-012), which
// removes every managed target with no confirmation outside the wizard. The
// CLI has no syntax for that explicit-empty selection, so it must be
// rejected as a usage error instead of silently wiping the project.
func TestInstall_AgentEqualsEmptyIsUsageError(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude,codex"); code != 0 {
		t.Fatalf("baseline install: code %d, stderr %q", code, stderr)
	}
	before, err := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent=")
	if code != 2 {
		t.Fatalf("code = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr, "--agent") {
		t.Errorf("stderr should mention --agent: %q", stderr)
	}
	after, _ := os.ReadFile(filepath.Join(dir, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if string(before) != string(after) {
		t.Error("--agent= modified the lock despite being rejected")
	}
	agentDirsExist(t, dir, "claude", "codex")
}

// TestInstall_FailedSkillOmitsAgentDiffFields (code-review fix): a skill
// whose removal step fails must not report agentsKept/Added/Removed or a
// "removed for" diagnostic — those describe intent, and the two-phase
// removal guarantees nothing was actually removed when a skill fails.
func TestInstall_FailedSkillOmitsAgentDiffFields(t *testing.T) {
	t.Parallel()
	src := addSourceTree(t, "alpha")
	dir := t.TempDir()
	hash, err := integrity.CompatHash(filepath.Join(src, "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	lock := `{"version":1,"skills":{"alpha":{"source":"` + strings.ReplaceAll(src, `\`, `\\`) + `","sourceType":"local","skillPath":"skills/alpha/SKILL.md","computedHash":"` + hash + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude,codex", "--install-mode", "copy"); code != 0 {
		t.Fatalf("baseline install: code %d, stderr %q", code, stderr)
	}

	// Hand-edit codex's copy-mode content so its removal fails.
	target := filepath.Join(dir, ".codex", "skills", "alpha", "SKILL.md")
	if err := os.WriteFile(target, []byte("---\nname: alpha\ndescription: hand-edited\n---\n# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--json")
	if code == 0 {
		t.Fatalf("narrow over foreign-modified content succeeded, want failure; stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stderr, "removed for") {
		t.Errorf("stderr falsely claims something was removed: %q", stderr)
	}
	var doc struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, stdout)
	}
	for _, s := range doc.Skills {
		if _, present := s["agentsRemoved"]; present {
			t.Errorf("failed skill JSON entry has agentsRemoved: %+v", s)
		}
	}
}

// TestInstall_JSONShape (T026): --json emits the documented result document.
func TestInstall_JSONShape(t *testing.T) {
	t.Parallel()
	dir := lockOnlyProject(t)
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var doc struct {
		Changed     bool     `json:"changed"`
		Initialized bool     `json:"initialized"`
		Migrated    bool     `json:"migrated"`
		Agents      []string `json:"agents"`
		Skills      []struct {
			Name         string `json:"name"`
			Status       string `json:"status"`
			ComputedHash string `json:"computedHash"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, stdout)
	}
	if !doc.Changed || !doc.Initialized {
		t.Errorf("doc = %+v, want changed+initialized", doc)
	}
	if len(doc.Agents) != 1 || doc.Agents[0] != "claude" {
		t.Errorf("agents = %v", doc.Agents)
	}
	if len(doc.Skills) != 2 || doc.Skills[0].Status != "installed" || len(doc.Skills[0].ComputedHash) != 64 {
		t.Errorf("skills = %+v", doc.Skills)
	}
}

// TestInstall_WizardPathPrintsNoDuplicateSummary (spec 014 FR-020, acceptance
// 15): the wizard's result screen already reported the run, so the CLI must
// not print the generic human summary again — and the partial-failure error
// must still map to exit 10, marked reported so Run skips its "error:" line.
func TestInstall_WizardPathPrintsNoDuplicateSummary(t *testing.T) { //nolint:paralleltest // swaps the global runLockWizardFn stub
	old := runLockWizardFn
	t.Cleanup(func() { runLockWizardFn = old })
	partial := fmt.Errorf("%w: 1 of 2 skills failed", errs.ErrPartialInstall)
	runLockWizardFn = func(_ context.Context, _ tui.LockWizardConfig, _ bool) (tui.LockWizardOutcome, error) {
		return tui.LockWizardOutcome{
			Executed: true,
			AgentIDs: []string{"claude"},
			Result: app.InstallFromLockResult{Skills: []app.LockSkillResult{
				{Name: "alpha", Status: app.LockSkillInstalled},
				{Name: "beta", Status: app.LockSkillFailed, Err: partial},
			}},
			Err: partial,
		}, nil
	}

	var stdout, stderr bytes.Buffer
	out := NewOutput(&stdout, &stderr, OutputOptions{Interactive: true})
	c := installCmd{}
	err := c.runLockWizard(context.Background(), out, app.New(app.Options{}), t.TempDir(), app.LockPreview{}, Globals{})
	if err == nil {
		t.Fatal("wizard partial failure returned nil error")
	}
	if errs.ExitCode(err) != int(errs.CodePartialInstall) {
		t.Errorf("exit code = %d, want %d", errs.ExitCode(err), errs.CodePartialInstall)
	}
	var rep reportedError
	if !errors.As(err, &rep) {
		t.Error("error not marked reported: Run would print a duplicate generic summary")
	}
	if got := stdout.String(); got != "" {
		t.Errorf("wizard path wrote a duplicate summary to stdout:\n%s", got)
	}
	if strings.Contains(stderr.String(), "Installed") {
		t.Errorf("wizard path wrote a duplicate summary to stderr:\n%s", stderr.String())
	}
}

// TestInstall_CancelledRunMapsToExit130 (spec 014 FR-025): a cancelled
// install maps to the existing cancellation exit code, is reported plainly
// (not as an "error:" line), and the non-wizard path emits no
// cursor-control/alternate-screen sequences.
func TestInstall_CancelledRunMapsToExit130(t *testing.T) {
	t.Parallel()
	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, OutputOptions{})
	code := reportRunError(out, fmt.Errorf("%w: installation interrupted: 2 of 3 skills not attempted", errs.ErrCancelled))
	if code != int(errs.CodeCancelled) {
		t.Errorf("exit code = %d, want %d", code, errs.CodeCancelled)
	}
	if strings.Contains(errb.String(), "error:") {
		t.Errorf("cancellation reported as an error line: %q", errb.String())
	}
	if !strings.Contains(errb.String(), "interrupted") {
		t.Errorf("cancellation not reported at all: %q", errb.String())
	}
	combined := outb.String() + errb.String()
	if strings.Contains(combined, "\x1b[") || strings.Contains(combined, "\x1b]") {
		t.Errorf("non-TTY cancellation output carries escape sequences: %q", combined)
	}
}

// TestReportRunError_RawContextCanceledIsCancellation (review C2): a raw
// context.Canceled chain (e.g. a git fetch aborted by the add wizard's esc)
// maps to the plain cancellation report and exit 130, never a generic error.
func TestReportRunError_RawContextCanceledIsCancellation(t *testing.T) {
	t.Parallel()
	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, OutputOptions{})
	code := reportRunError(out, fmt.Errorf("git: fetch: %w", context.Canceled))
	if code != int(errs.CodeCancelled) {
		t.Errorf("exit code = %d, want %d", code, errs.CodeCancelled)
	}
	if strings.Contains(errb.String(), "error:") {
		t.Errorf("raw cancellation reported as an error line: %q", errb.String())
	}
}
