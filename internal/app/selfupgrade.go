package app

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/version"
)

// SelfUpgradeRequest carries the user's choices for `gskill self upgrade`.
type SelfUpgradeRequest struct {
	// To is the exact release requested with --to, or empty for the latest
	// stable release.
	To string
	// NoCache keeps the Sigstore TUF metadata in memory only.
	NoCache bool
}

// SelfUpgradePlan decides what the upgrade would do without writing anything.
func (a *App) SelfUpgradePlan(ctx context.Context, req SelfUpgradeRequest) (selfupdate.UpgradePlan, error) {
	return selfupdate.Plan(ctx, a.selfUpdateEnv(req), selfupdate.PlanRequest{To: req.To})
}

// SelfUpgradeApply carries out an upgrade plan, reporting each verified step
// to progress.
func (a *App) SelfUpgradeApply(ctx context.Context, plan selfupdate.UpgradePlan, req SelfUpgradeRequest, progress func(string)) (selfupdate.Outcome, error) {
	return selfupdate.Apply(ctx, a.selfUpdateEnv(req), plan, progress)
}

func (a *App) selfUpdateEnv(req SelfUpgradeRequest) selfupdate.Env {
	if a.selfUpdate != nil {
		return *a.selfUpdate
	}
	// Without a resolvable cache directory the TUF metadata stays in memory;
	// the upgrade itself does not need the cache.
	cacheDir, err := config.CacheDir()
	if err != nil {
		cacheDir = ""
	}
	args0 := ""
	if len(os.Args) > 0 {
		args0 = os.Args[0]
	}
	return selfupdate.Env{
		Source:     selfupdate.NewGitHubSource(nil, selfupdate.DefaultAPIBase, os.Getenv("GITHUB_TOKEN")),
		Verifier:   selfupdate.NewSigstoreVerifier(cacheDir, req.NoCache),
		Executable: os.Executable,
		Args0:      args0,
		LookPath:   exec.LookPath,
		Getenv:     os.Getenv,
		ReadFile:   os.ReadFile,
		Build: selfupdate.BuildInfo{
			Version:    version.Version(),
			Kind:       version.Kind(),
			ModulePath: version.ModulePath(),
		},
		Probe: func(ctx context.Context, path string) (string, error) {
			return selfupdate.ProbeVersion(ctx, path, 10*time.Second)
		},
		LockTimeout: time.Minute,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
	}
}
