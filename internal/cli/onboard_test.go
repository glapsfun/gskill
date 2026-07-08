package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/tui"
)

// Spec 011 T034: `gskill onboard` — the source-less wizard entry point.

func TestOnboard_NonInteractiveIsUsageErrorWithHint(t *testing.T) {
	t.Parallel()

	dir := initedProject(t)
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "onboard")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage) in a non-interactive session\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "gskill add") {
		t.Errorf("stderr = %q, want a hint to use 'gskill add' with flags", stderr)
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestOnboard_TTYLaunchesSourcelessWizard(t *testing.T) {
	dir := initedProject(t)

	var gotCfg *tui.WizardConfig
	withWizardSeams(t, true, func(_ context.Context, cfg tui.WizardConfig, _ bool) (tui.WizardOutcome, error) {
		gotCfg = &cfg
		return tui.WizardOutcome{Cancelled: true}, nil
	})

	var out, errb bytes.Buffer
	c := onboardCmd{}
	_ = c.Run(context.Background(), interactiveOutput(&out, &errb), newTestApp(), projectRoot(dir), Globals{})

	if gotCfg == nil {
		t.Fatal("onboard did not launch the wizard on a TTY")
	}
	if gotCfg.Session.SourceAnswered || gotCfg.Session.Source != "" {
		t.Errorf("onboard must start with no source answered: %+v", gotCfg.Session)
	}
	if gotCfg.Phases.ValidateSource == nil {
		t.Error("onboard wizard missing the source validator")
	}
}

//nolint:paralleltest // swaps package-level wizard seams
func TestOnboard_JSONModeIsUsageError(t *testing.T) {
	dir := initedProject(t)

	wizardCalled := false
	withWizardSeams(t, true, func(context.Context, tui.WizardConfig, bool) (tui.WizardOutcome, error) {
		wizardCalled = true
		return tui.WizardOutcome{}, nil
	})

	var out, errb bytes.Buffer
	o := &Output{stdout: &out, stderr: &errb, interactive: true, json: true}
	err := onboardCmd{}.Run(context.Background(), o, newTestApp(), projectRoot(dir), Globals{})
	if err == nil {
		t.Fatal("onboard in --json mode succeeded, want usage error")
	}
	if wizardCalled {
		t.Error("onboard launched the wizard in --json mode")
	}
}
