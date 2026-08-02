package integration_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/git"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// authFailRunner is a git.Runner that always fails authentication (redacted).
type authFailRunner struct{}

func (authFailRunner) LsRemoteTags(context.Context, string) ([]git.TagRef, error) {
	return nil, errs.Wrap(errs.CodeAuth, "git authentication failed: //***@github.com", nil)
}

func (authFailRunner) LsRemoteHeads(context.Context, string) ([]git.BranchRef, error) {
	return nil, errs.Wrap(errs.CodeAuth, "git authentication failed: //***@github.com", nil)
}

func (authFailRunner) ResolveRef(context.Context, string, string) (string, error) {
	return "", errs.Wrap(errs.CodeAuth, "git authentication failed: //***@github.com", nil)
}

func (authFailRunner) FetchCommit(context.Context, string, string, string) error {
	return errs.Wrap(errs.CodeAuth, "git authentication failed: //***@github.com", nil)
}

func TestExitCodes_AuthFailureIsExit11Redacted(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	a := app.New(app.Options{Agents: agent.NewDefaultRegistry(), Git: authFailRunner{}, Logger: discardLogger()})

	initProjectWithApp(t, a, proj)

	_, stderr, code := runGskillWithApp(t, a, proj, "add", "github.com/acme/demo", "--version", "^1.0.0")
	if code != 11 {
		t.Errorf("exit code = %d, want 11 (authentication failure)", code)
	}
	if strings.Contains(stderr, "hunter2") || strings.Contains(strings.ToLower(stderr), "password=") {
		t.Errorf("stderr appears to leak a credential: %q", stderr)
	}
}

// failValidateAgent activates fine but fails post-install validation, forcing a
// partial-installation outcome.
type failValidateAgent struct{}

func (failValidateAgent) ID() string          { return "fail-agent" }
func (failValidateAgent) DisplayName() string { return "Fail Agent" }
func (failValidateAgent) ProjectSkillDir(root string) string {
	return filepath.Join(root, ".fail", "skills")
}

func (failValidateAgent) GlobalSkillDir(home string) string {
	return filepath.Join(home, ".fail", "skills")
}
func (failValidateAgent) SupportsSymlinks() bool { return true }
func (failValidateAgent) Detect(context.Context, string) (bool, error) {
	return false, nil
}

func (failValidateAgent) ValidateInstallation(context.Context, string) error {
	return errs.New(errs.CodeGeneric, "agent rejected the installed skill")
}

func TestOutdated_MatchesUpdateListEligibility(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	pinRepo := gitRepo(t, validSkill("deploy"), "v1.0.0", "v2.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", pinRepo, "--ref", "v1.0.0"); code != 0 {
		t.Fatalf("add pin: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")

	// The report rows are primary output: stdout, not stderr (FR-017), and
	// eligibility matches update --list exactly (FR-008, SC-007).
	outdatedOut, _, code := runGskill(t, proj, "outdated")
	if code != 0 {
		t.Fatalf("outdated exit = %d, want 0", code)
	}
	listOut, _, code := runGskill(t, proj, "update", "--list")
	if code != 0 {
		t.Fatalf("update --list exit = %d, want 0", code)
	}
	for _, out := range []string{outdatedOut, listOut} {
		if !strings.Contains(out, "demo") {
			t.Errorf("actionable skill missing from stdout report:\n%s", out)
		}
		if strings.Contains(out, "deploy") {
			t.Errorf("pinned tag reported as actionable:\n%s", out)
		}
	}
}

func TestOutdated_ExitCode8OnlyForActionableUpdates(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	if _, _, code := runGskill(t, proj, "outdated", "--exit-code"); code != 0 {
		t.Errorf("up-to-date project: outdated --exit-code = %d, want 0", code)
	}

	publishNewVersion(t, repo, "demo", "v1.1.0")
	if _, _, code := runGskill(t, proj, "outdated", "--exit-code"); code != 8 {
		t.Errorf("actionable update: outdated --exit-code = %d, want 8", code)
	}
}

func TestOutdated_ExactTagPinNeverTriggersExit8(t *testing.T) {
	t.Parallel()

	// Regression: an exact tag pin with newer upstream tags used to report
	// exit 8 even though `gskill update` preserves the pin (FR-004).
	pinRepo := gitRepo(t, validSkill("deploy"), "v1.0.0", "v2.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", pinRepo, "--ref", "v1.0.0"); code != 0 {
		t.Fatalf("add pin: %s", stderr)
	}

	if _, _, code := runGskill(t, proj, "outdated", "--exit-code"); code != 0 {
		t.Errorf("pinned-only project: outdated --exit-code = %d, want 0", code)
	}
}

func TestOutdated_DiscoveryFailureNeverExitsLikeVerified(t *testing.T) {
	t.Parallel()

	// Regression (review round 2): with the remote unreachable, discovery
	// downgrades to per-skill unknown items — the report renders, but the
	// exit code must not match a verified up-to-date project, or CI gates
	// pass green during an outage.
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runGskill(t, proj, "outdated")
	if code == 0 || code == 8 {
		t.Errorf("outdated during outage exit = %d, want a distinct non-zero failure", code)
	}
	if !strings.Contains(stdout, "could not be checked") {
		t.Errorf("outdated stdout hides the unchecked state:\n%s", stdout)
	}
	if _, _, ecode := runGskill(t, proj, "outdated", "--exit-code"); ecode == 0 || ecode == 8 {
		t.Errorf("outdated --exit-code during outage = %d, want a distinct non-zero failure", ecode)
	}
	if _, _, lcode := runGskill(t, proj, "update", "--list"); lcode == 0 {
		t.Errorf("update --list during outage exit = %d, want non-zero", lcode)
	}
}

func TestOutdated_PinOnlyOutageStaysExitZero(t *testing.T) {
	t.Parallel()

	// A project holding only exact pins is fully classified by the lock: a
	// dead remote breaks nothing actionable, so the report must exit 0 and
	// stay self-consistent — the failed informational lookup shows in the
	// --all STATUS cell instead of flipping the exit code.
	pinRepo := gitRepo(t, validSkill("deploy"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", pinRepo, "--ref", "v1.0.0"); code != 0 {
		t.Fatalf("add pin: %s", stderr)
	}
	if err := os.RemoveAll(pinRepo); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runGskill(t, proj, "update", "--list")
	if code != 0 {
		t.Errorf("pin-only outage: update --list exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "All skills are up to date") {
		t.Errorf("pin-only list summary changed:\n%s", stdout)
	}
	if _, _, ecode := runGskill(t, proj, "outdated", "--exit-code"); ecode != 0 {
		t.Errorf("pin-only outage: outdated --exit-code = %d, want 0", ecode)
	}
	allOut, _, code := runGskill(t, proj, "update", "--list", "--all")
	if code != 0 {
		t.Errorf("pin-only outage: --list --all exit = %d, want 0", code)
	}
	if !strings.Contains(allOut, "lookup failed") {
		t.Errorf("--all hides the failed informational lookup:\n%s", allOut)
	}
}

func TestExitCodes_PartialInstallationIsExit10(t *testing.T) {
	t.Parallel()

	reg := agent.NewRegistry()
	if err := reg.Register(failValidateAgent{}); err != nil {
		t.Fatal(err)
	}
	a := app.New(app.Options{Agents: reg, Logger: discardLogger()})

	proj := newProject(t)
	skill := localSkillDir(t, "demo")

	initProjectWithApp(t, a, proj)

	_, _, code := runGskillWithApp(t, a, proj, "add", skill, "--agent", "fail-agent")
	if code != 10 {
		t.Errorf("exit code = %d, want 10 (partial installation)", code)
	}
}
