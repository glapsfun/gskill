package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/testutil"
)

// renderResults captures renderLockInstall's stdout for a fabricated result.
func renderResults(t *testing.T, res app.InstallFromLockResult, opts OutputOptions) (stdout, stderr string) {
	t.Helper()
	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, opts)
	if err := renderLockInstall(out, res, false); err != nil {
		t.Fatalf("renderLockInstall: %v", err)
	}
	return outb.String(), errb.String()
}

func renderFailure(name, source, version string, cat app.FailureCategory, phase app.InstallPhase, msg, hint string) app.LockSkillResult {
	return app.LockSkillResult{
		Name: name, Source: source, Status: app.LockSkillFailed,
		SourceType: "github", SkillPath: "skills/" + name,
		ResolvedVersion: version, Phase: phase,
		Err: errors.New(msg),
		Failure: &app.InstallFailure{
			Category: cat, Phase: phase, Message: msg, Hint: hint,
		},
	}
}

func successResult(name, status string) app.LockSkillResult {
	return app.LockSkillResult{
		Name: name, Source: "github.com/acme/skills", Status: status,
		SourceType: "github", ResolvedVersion: "1.2.3", Commit: "a1b2c3d4e5f6",
	}
}

func cancelledResults() app.InstallFromLockResult {
	interrupted := app.LockSkillResult{
		Name: "beta", Source: "github.com/acme/skills", SourceType: "github",
		Status: string(app.InstallStatusCancelled), Phase: app.InstallPhaseFetching,
		Err: errors.New("git: context canceled"),
		Failure: &app.InstallFailure{
			Category: app.FailureCancelled, Phase: app.InstallPhaseFetching,
			Message: "git: context canceled",
		},
	}
	return app.InstallFromLockResult{
		Agents: []string{"claude"}, Changed: true,
		Skills: []app.LockSkillResult{
			successResult("alpha", app.LockSkillInstalled),
			interrupted,
			{
				Name: "gamma", Source: "github.com/acme/skills", SourceType: "github",
				Status: string(app.InstallStatusNotAttempted),
			},
		},
	}
}

func partialResults() app.InstallFromLockResult {
	return app.InstallFromLockResult{
		Agents:  []string{"claude"},
		Changed: true,
		Skills: []app.LockSkillResult{
			successResult("alpha", app.LockSkillInstalled),
			successResult("beta", app.LockSkillRepaired),
			renderFailure("gke-scaling", "glapsfun/cloud-native-skills", "v1.4.2",
				app.FailureIntegrity, app.InstallPhaseVerifying,
				"computedHash mismatch: lock records sha256:03e0, source content is sha256:94ab",
				"re-run with --force to accept the changed upstream content"),
			renderFailure("k8s-debug", "./local-skills", "",
				app.FailureForeignContent, app.InstallPhaseLinking,
				"destination already exists and is not managed by gskill", ""),
		},
	}
}

// TestInstallRender_PlainGoldens (FR-021, SC-006): plain output carries the
// summary counters and equivalent per-failure blocks, pinned by goldens.
func TestInstallRender_PlainGoldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		golden string
		res    app.InstallFromLockResult
	}{
		{
			name:   "success",
			golden: "install-plain-success.golden",
			res: app.InstallFromLockResult{
				Agents: []string{"claude"}, Changed: true,
				Skills: []app.LockSkillResult{
					successResult("alpha", app.LockSkillInstalled),
					successResult("beta", app.LockSkillInstalled),
					successResult("gamma", app.LockSkillUpToDate),
				},
			},
		},
		{
			name:   "partial",
			golden: "install-plain-partial.golden",
			res:    partialResults(),
		},
		{
			name:   "total-failure",
			golden: "install-plain-total-failure.golden",
			res: app.InstallFromLockResult{
				Agents: []string{"claude"},
				Skills: []app.LockSkillResult{
					renderFailure("alpha", "github.com/acme/skills", "",
						app.FailureSourceUnavailable, app.InstallPhaseResolving,
						"source unavailable: dial tcp: no such host", ""),
					renderFailure("beta", "github.com/acme/skills", "",
						app.FailureSourceUnavailable, app.InstallPhaseResolving,
						"source unavailable: dial tcp: no such host", ""),
				},
			},
		},
		{
			name:   "cancelled",
			golden: "install-plain-cancelled.golden",
			res:    cancelledResults(),
		},
		{
			name:   "dry-run",
			golden: "install-plain-dry-run.golden",
			res: app.InstallFromLockResult{
				Agents: []string{"claude"},
				Skills: []app.LockSkillResult{
					{Name: "fresh", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldInstall},
					{Name: "relink", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldRepair},
					{Name: "narrow", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldRemoveTarget},
					{Name: "rewrite", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldUpdateLock},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, _ := renderResults(t, tt.res, OutputOptions{})
			testutil.Golden(t, tt.golden, []byte(stdout))
		})
	}
}

// TestInstallRender_CountersSumAndNoContradiction (FR-015/FR-016): the old
// "Installed 0 skill(s), 3 failed" contradiction is structurally gone.
func TestInstallRender_CountersSumAndNoContradiction(t *testing.T) {
	t.Parallel()
	stdout, _ := renderResults(t, partialResults(), OutputOptions{})
	for _, want := range []string{"4 skills processed", "1 installed", "1 repaired", "2 failed"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Installed 0 skill(s)") {
		t.Errorf("contradictory zero-install line survived:\n%s", stdout)
	}
}

// TestInstallRender_FailureBlocksCarryEquivalentInfo (FR-021): each failure
// block names skill, source, version (— when unknown), phase, reason, hint.
func TestInstallRender_FailureBlocksCarryEquivalentInfo(t *testing.T) {
	t.Parallel()
	stdout, _ := renderResults(t, partialResults(), OutputOptions{})
	for _, want := range []string{
		"FAILED", "gke-scaling", "glapsfun/cloud-native-skills", "v1.4.2",
		"verifying", "computedHash mismatch",
		"re-run with --force",
		"k8s-debug", "—", "linking", "not managed by gskill",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("failure blocks missing %q:\n%s", want, stdout)
		}
	}
}

// TestInstallRender_InteractiveMatchesPlainBytes: stdout is not a TTY in
// either case, so the "colored" path must degrade to byte-identical output
// (NO_COLOR / piped contract, FR-027).
func TestInstallRender_InteractiveMatchesPlainBytes(t *testing.T) {
	t.Parallel()
	plain, _ := renderResults(t, partialResults(), OutputOptions{})
	styled, _ := renderResults(t, partialResults(), OutputOptions{Interactive: true})
	if plain != styled {
		t.Errorf("piped output differs between interactive and plain construction:\n--- plain ---\n%s\n--- styled ---\n%s", plain, styled)
	}
}

// TestInstallRender_SanitizesUntrustedText (FR-028/SC-008): hostile metadata
// cannot emit escape sequences through the plain renderer.
func TestInstallRender_SanitizesUntrustedText(t *testing.T) {
	t.Parallel()
	res := app.InstallFromLockResult{Skills: []app.LockSkillResult{
		renderFailure("evil\x1b]0;pwned\x07", "src\x1b[2Jwipe", "v1\x1b[31m",
			app.FailureUnknown, app.InstallPhaseVerifying, "boom \x1b[31mred\x1b[0m", "hint\x1b]8;;x\x07"),
	}}
	stdout, stderr := renderResults(t, res, OutputOptions{})
	for _, s := range []string{stdout, stderr} {
		if strings.Contains(s, "\x1b") || strings.Contains(s, "\x07") {
			t.Errorf("escape bytes leaked into plain output:\n%q", s)
		}
	}
}
