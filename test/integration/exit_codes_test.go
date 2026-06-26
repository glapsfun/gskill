package integration_test

import (
	"context"
	"io"
	"log/slog"
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

	if _, stderr, code := runGskillWithApp(t, a, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}

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

func TestExitCodes_PartialInstallationIsExit10(t *testing.T) {
	t.Parallel()

	reg := agent.NewRegistry()
	if err := reg.Register(failValidateAgent{}); err != nil {
		t.Fatal(err)
	}
	a := app.New(app.Options{Agents: reg, Logger: discardLogger()})

	proj := newProject(t)
	skill := localSkillDir(t, "demo")

	if _, stderr, code := runGskillWithApp(t, a, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}

	_, _, code := runGskillWithApp(t, a, proj, "add", skill, "--agent", "fail-agent")
	if code != 10 {
		t.Errorf("exit code = %d, want 10 (partial installation)", code)
	}
}
