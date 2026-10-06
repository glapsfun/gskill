package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// modeShared is the per-agent lock mode of a shared-location target.
const modeShared = "shared"

// sharedFakeAgent is a test-only shared-location agent (spec 027): its project
// skill dir is the repo-owned store itself, like Antigravity CLI or OpenCode.
type sharedFakeAgent struct{}

func (sharedFakeAgent) ID() string                                   { return "sharedfake" }
func (sharedFakeAgent) DisplayName() string                          { return "Shared Fake" }
func (sharedFakeAgent) Detect(context.Context, string) (bool, error) { return false, nil }
func (sharedFakeAgent) ProjectSkillDir(root string) string {
	return filepath.Join(root, ".agents", "skills")
}

func (sharedFakeAgent) GlobalSkillDir(home string) string {
	return filepath.Join(home, ".sharedfake", "skills")
}
func (sharedFakeAgent) SupportsSymlinks() bool { return true }
func (sharedFakeAgent) ValidateInstallation(_ context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return errors.New("SKILL.md missing in " + dir)
	}
	return nil
}

// appWithShared builds an App whose registry holds Claude plus the shared fake.
func appWithShared(t *testing.T) *app.App {
	t.Helper()
	reg := agent.NewRegistry()
	_ = reg.Register(agent.NewClaudeCode())
	_ = reg.Register(sharedFakeAgent{})
	return app.New(app.Options{
		Agents:     reg,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: filepath.Join(t.TempDir(), "gskill-home"),
	})
}

// sharedLock is the slice of skills-lock.json these tests inspect.
type sharedLock struct {
	Skills map[string]struct {
		Gskill struct {
			InstallMode string `json:"installMode"`
			State       struct {
				Targets map[string]string `json:"targets"`
				Modes   map[string]string `json:"modes"`
			} `json:"state"`
		} `json:"gskill"`
	} `json:"skills"`
}

func readSharedLock(t *testing.T, proj string) sharedLock {
	t.Helper()
	var lf sharedLock
	if err := json.Unmarshal([]byte(readLock(t, proj)), &lf); err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	return lf
}

// sharedProject returns a fresh project with "demo" added for claude and the
// shared fake, plus the App that installed it.
func sharedProject(t *testing.T) (*app.App, string) {
	t.Helper()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	a := appWithShared(t)
	initProjectWithApp(t, a, proj)
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude", "--agent", "sharedfake"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	requireStoreContent(t, proj, "demo")
	return a, proj
}

func requireStoreContent(t *testing.T, proj, name string) {
	t.Helper()
	dir := filepath.Join(proj, ".agents", "skills", name)
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatalf("store entry missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("store entry %s became a symlink", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("store entry lost its content: %v", err)
	}
}

func TestSharedTarget_RecordedAgainstTheStore(t *testing.T) {
	t.Parallel()
	_, proj := sharedProject(t)

	entry := readSharedLock(t, proj).Skills["demo"].Gskill
	if got := entry.State.Targets["sharedfake"]; got != ".agents/skills/demo" {
		t.Errorf("shared target = %q, want .agents/skills/demo", got)
	}
	if got := entry.State.Modes["sharedfake"]; got != modeShared {
		t.Errorf("shared mode = %q, want shared", got)
	}
	if entry.InstallMode == modeShared {
		t.Error("installMode recorded as shared; it must stay the linking agents' placement")
	}
}

func TestSharedTarget_DroppingTheAgentKeepsContent(t *testing.T) {
	t.Parallel()
	a, proj := sharedProject(t)

	if _, stderr, code := runGskillWithApp(t, a, proj, "install", "--agent", "claude"); code != 0 {
		t.Fatalf("install --agent claude: %s", stderr)
	}
	requireStoreContent(t, proj, "demo")
	state := readSharedLock(t, proj).Skills["demo"].Gskill.State
	if _, ok := state.Targets["sharedfake"]; ok {
		t.Error("lock still records a sharedfake target after the agent was dropped")
	}
	if _, ok := state.Modes["sharedfake"]; ok {
		t.Error("lock still records a sharedfake mode after the agent was dropped")
	}
}

func TestSharedTarget_CheckIsCleanAfterInstall(t *testing.T) {
	t.Parallel()
	a, proj := sharedProject(t)

	if _, stderr, code := runGskillWithApp(t, a, proj, "project", "check", "--fail-on-drift"); code != 0 {
		t.Fatalf("check = %d, want 0: %s", code, stderr)
	}
}

func TestSharedTarget_TamperIsOneProblemLine(t *testing.T) {
	t.Parallel()
	a, proj := sharedProject(t)

	f, err := os.OpenFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"), os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // test-owned temp project
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runGskillWithApp(t, a, proj, "project", "check", "--fail-on-drift")
	if code != 7 {
		t.Fatalf("check = %d, want 7: %s", code, stderr)
	}
	if strings.Contains(stderr, "sharedfake:demo") {
		t.Errorf("shared target reported separately from the store drift:\n%s", stderr)
	}
	if n := strings.Count(stderr, "no longer matches skills-lock.json"); n != 1 {
		t.Errorf("drift reported %d times, want once:\n%s", n, stderr)
	}
}

func TestSharedTarget_MissingStoreIsNotClean(t *testing.T) {
	t.Parallel()
	a, proj := sharedProject(t)

	if err := os.RemoveAll(filepath.Join(proj, ".agents", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskillWithApp(t, a, proj, "project", "check", "--fail-on-drift"); code != 7 {
		t.Fatalf("check = %d, want 7: %s", code, stderr)
	}
}

func TestSharedTarget_SyncLeavesTheStoreAlone(t *testing.T) {
	t.Parallel()
	a, proj := sharedProject(t)

	if _, stderr, code := runGskillWithApp(t, a, proj, "project", "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}
	requireStoreContent(t, proj, "demo")
}

func TestSharedTarget_RemoveDeletesContent(t *testing.T) {
	t.Parallel()
	a, proj := sharedProject(t)

	if _, stderr, code := runGskillWithApp(t, a, proj, "--yes", "remove", "demo"); code != 0 {
		t.Fatalf("remove: %s", stderr)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".agents", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf("store entry survived remove: %v", err)
	}
	if _, ok := readSharedLock(t, proj).Skills["demo"]; ok {
		t.Error("lock entry survived remove")
	}
}

// TestSharedTarget_ExplicitModeIsStable guards the Research D3 rule: with
// mode = "copy" declared and only shared targets, a recorded "shared" mode
// would read as a placement change on every run, so plain install would never
// settle and --frozen-lockfile would refuse forever.
func TestSharedTarget_ExplicitModeIsStable(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	a := appWithShared(t)
	initProjectWithApp(t, a, proj)
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo, "--version", "^1.0.0", "--agent", "sharedfake", "--copy"); code != 0 {
		t.Fatalf("add --copy: %s", stderr)
	}
	requireStoreContent(t, proj, "demo")

	if _, stderr, code := runGskillWithApp(t, a, proj, "install"); code != 0 {
		t.Fatalf("first install: %s", stderr)
	}
	stdout, stderr, code := runGskillWithApp(t, a, proj, "install", "--json")
	if code != 0 {
		t.Fatalf("second install: %s", stderr)
	}
	var res struct {
		Skills []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse install --json: %v\n%s", err, stdout)
	}
	if len(res.Skills) != 1 || res.Skills[0].Status != app.LockSkillUpToDate {
		t.Errorf("second install skills = %+v, want demo %s", res.Skills, app.LockSkillUpToDate)
	}
	if _, stderr, code := runGskillWithApp(t, a, proj, "install", "--frozen-lockfile"); code != 0 {
		t.Fatalf("install --frozen-lockfile = %d, want 0: %s", code, stderr)
	}
}

// TestSharedAgents_AllFourTogether covers spec 027 SC-003 with the real
// adapters: one install for every shared-location agent writes nothing outside
// the store and gskill's own files.
func TestSharedAgents_AllFourTogether(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	before := topLevel(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"),
		"--agent", "antigravity", "--agent", "opencode", "--agent", "openclaw", "--agent", "hermes"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	requireOnlyGskillEntries(t, proj, before)
	requireStoreContent(t, proj, "demo")
	modes := readSharedLock(t, proj).Skills["demo"].Gskill.State.Modes
	for _, id := range []string{"antigravity", "opencode", "openclaw", "hermes"} {
		if modes[id] != modeShared {
			t.Errorf("%s mode = %q, want shared", id, modes[id])
		}
	}
	if _, stderr, code := runGskill(t, proj, "project", "check", "--fail-on-drift"); code != 0 {
		t.Errorf("check = %d, want 0: %s", code, stderr)
	}
}

// TestSharedAgents_ExplicitModeOnAgentAddIsStable covers the stand-in mode on
// an agent-add (spec 027 Research D3): adding a second shared agent with
// --copy declares mode = "copy", and the lock must agree, or every later
// install would see a placement change and --frozen-lockfile would refuse.
func TestSharedAgents_ExplicitModeOnAgentAddIsStable(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	src := localSkillDir(t, "demo")
	if _, stderr, code := runGskill(t, proj, "add", src, "--agent", "opencode"); code != 0 {
		t.Fatalf("add --agent opencode: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", src, "--agent", "hermes", "--copy"); code != 0 {
		t.Fatalf("add --agent hermes --copy: %s", stderr)
	}
	// Checked straight after the add: a mismatch would heal on the next full
	// install, so only the immediate runs reveal it.
	if _, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile"); code != 0 {
		t.Errorf("install --frozen-lockfile = %d, want 0: %s", code, stderr)
	}
	statuses, _ := installJSON(t, proj)
	if statuses["demo"] != app.LockSkillUpToDate {
		t.Errorf("demo status = %q right after the add, want %s", statuses["demo"], app.LockSkillUpToDate)
	}
}
