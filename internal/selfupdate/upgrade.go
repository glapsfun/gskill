package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/sys/unix"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
)

const (
	installDocs   = "https://github.com/glapsfun/gskill/blob/main/docs/installation.md"
	installScript = "curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh"
)

// Plan decides what a self-upgrade would do, without writing anything. A
// refusal comes back as an error. A package-managed install with a newer
// release returns its plan together with an "update available" error, so the
// caller can print the command and still exit 8.
func Plan(ctx context.Context, env Env, req PlanRequest) (UpgradePlan, error) {
	var requested *semver.Version
	if req.To != "" {
		v, err := semver.StrictNewVersion(strings.TrimPrefix(req.To, "v"))
		if err != nil {
			return UpgradePlan{}, errs.Wrap(errs.CodeUsage, fmt.Sprintf("--to %q is not a version; use X.Y.Z", req.To), err)
		}
		requested = v
	}

	exe, invokedAs, err := ResolveExecutable(env.Executable, env.Args0, env.LookPath)
	if err != nil {
		return UpgradePlan{}, err
	}
	inst := Detect(DetectInput{Executable: exe, InvokedAs: invokedAs, Build: env.Build, Getenv: env.Getenv, ReadFile: env.ReadFile})
	inst.OS, inst.Arch = env.GOOS, env.GOARCH
	p := UpgradePlan{Installation: inst, Requested: req.To}

	if inst.Channel == ChannelDevelopment {
		return p, errs.WithHint(errs.New(errs.CodeGeneric,
			fmt.Sprintf("this gskill is a development build (%s) and cannot upgrade itself", inst.Version)),
			"install a release: "+installDocs)
	}
	// Runs another gskill to read its version, so only once the run can go on.
	p.Installation.PathShadow = Shadow(ctx, env, exe)

	// A downgrade is refused before any network request: it needs no release
	// data, and an unpublished old version should still read as a downgrade.
	current, err := semver.NewVersion(strings.TrimPrefix(inst.Version, "v"))
	if err != nil {
		return p, errs.Wrap(errs.CodeGeneric, fmt.Sprintf("cannot compare the installed version %q", inst.Version), err)
	}
	if requested != nil && requested.LessThan(current) {
		return p, errs.WithHint(errs.New(errs.CodeUsage,
			fmt.Sprintf("%s is older than the installed %s; downgrades are not supported", requested, strings.TrimPrefix(inst.Version, "v"))),
			"install an older version with the install script and VERSION=v"+requested.String())
	}

	target, err := lookupTarget(ctx, env.Source, requested)
	if err != nil {
		return p, err
	}
	p.Target = &target

	if !target.Version.GreaterThan(current) {
		p.Action = ActionNone
		return p, nil
	}
	if inst.Channel != ChannelStandalone {
		p.Action = ActionPrintAction
		pin := ""
		if requested != nil {
			pin = requested.String()
		}
		p.NextCommand = NextCommand(p.Installation, pin)
		return p, errs.Wrap(errs.CodeUpdateAvailable, "gskill "+target.Version.String()+" is available", nil)
	}
	if err := checkWritableNoWrite(exe); err != nil {
		return p, err
	}
	p.Action = ActionUpgrade
	return p, nil
}

// lookupTarget returns the requested release, or the latest stable one.
func lookupTarget(ctx context.Context, src Source, requested *semver.Version) (Release, error) {
	if requested != nil {
		return src.ByTag(ctx, "v"+requested.String())
	}
	return src.Latest(ctx)
}

// checkWritableNoWrite asks the kernel whether the executable's directory is
// writable without creating anything, so a dry run and the prompt can report
// a permission problem before the user confirms. Replacing a file by rename
// needs write access to its directory only.
func checkWritableNoWrite(exe string) error {
	dir := filepath.Dir(exe)
	if err := unix.Access(dir, unix.W_OK); err != nil {
		return cannotWrite(exe, dir, err)
	}
	return nil
}

func permissionError(exe, dir string) error {
	return errs.WithHint(errs.New(errs.CodeGeneric, "cannot write "+exe+": permission denied"),
		"rerun as a user who can write "+dir+", or reinstall into a writable directory with the install script")
}

// NextCommand returns the lines that tell the user how the owner of inst
// upgrades it. pin is the version requested with --to, or "" for the latest.
func NextCommand(inst Installation, pin string) []string {
	ref := "latest"
	if pin != "" {
		ref = pin
	}
	npmGlobal := "npm install --global @glapsfun/gskill@" + ref
	npmProject := func(dir string) string {
		return "update the @glapsfun/gskill dependency of " + dir + ", for example: npm install --save-dev @glapsfun/gskill@" + ref
	}
	npx := "npx @glapsfun/gskill@" + ref + " <command>, or change the version pinned in your script"
	brew := []string{"brew upgrade glapsfun/tap/gskill"}
	goInstall := "go install github.com/glapsfun/gskill/cmd/gskill@latest"
	docker := "docker pull ghcr.io/glapsfun/gskill:" + ref
	script := installScript
	if pin != "" {
		// A cask cannot be pinned to a version, so say what brew will do.
		brew = append(brew, "Homebrew installs the tap's current version, which may not be "+pin+".")
		goInstall = "go install github.com/glapsfun/gskill/cmd/gskill@v" + pin
		script = strings.Replace(installScript, "| sh", "| VERSION=v"+pin+" sh", 1)
	}
	npmAll := []string{npmGlobal, npmProject("your project"), npx}
	goLines := []string{goInstall}
	if dir := filepath.Dir(inst.Executable); inst.GoInstallDir != "" && dir != inst.GoInstallDir {
		goLines = append(goLines, "`go install` writes to "+inst.GoInstallDir+", not to "+inst.Executable)
	}

	// Standalone and development builds have no owner to delegate to.
	return map[Channel][]string{
		ChannelHomebrew:   brew,
		ChannelNPMGlobal:  {npmGlobal},
		ChannelNPMProject: {npmProject(inst.NPMProjectDir)},
		ChannelNPX:        {npx},
		ChannelNPM:        npmAll,
		ChannelGo:         goLines,
		ChannelContainer:  {docker},
		ChannelUnknown:    append(append(slices.Clone(npmAll), brew...), goInstall, docker, script),
	}[inst.Channel]
}

// Apply carries out an ActionUpgrade plan. Every step must pass before the
// next starts, and until the final rename the executable is untouched.
func Apply(ctx context.Context, env Env, p UpgradePlan, progress func(string)) (Outcome, error) {
	out := baseOutcome(p)
	if p.Action != ActionUpgrade || p.Target == nil {
		return out, nil
	}
	if progress == nil {
		progress = func(string) {}
	}
	fail := func(err error) (Outcome, error) {
		err = interrupted(ctx, err)
		out.Kind, out.Err = OutcomeFailed, err
		return out, err
	}

	exe := p.Installation.Executable
	dir := filepath.Dir(exe)
	lock, err := fsutil.AcquireDir(ctx, dir, env.LockTimeout)
	if err != nil {
		// Only a timeout means another upgrade holds the lock; any other
		// failure keeps its own cause.
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			err = errs.WithHint(errs.New(errs.CodeCacheLock, "another gskill self upgrade is in progress for "+exe),
				"wait for it to finish, then run gskill version")
		}
		return fail(err)
	}
	defer func() { _ = lock.Release() }()

	if err := Cleanup(dir); err != nil {
		return fail(err)
	}
	// Another upgrade may have finished while this one waited for the lock.
	if got, err := env.Probe(ctx, exe); err == nil && !p.Target.Version.GreaterThan(parseOrZero(got)) {
		out.Kind, out.InstalledVersion = OutcomeUpToDate, got
		return out, nil
	}
	if err := CheckWritable(exe); err != nil {
		return fail(err)
	}
	if changed, err := apply(ctx, env, p, progress); err != nil {
		if changed {
			// The executable is not what it was; report that, not an interrupt.
			out.Kind, out.Err = OutcomeFailed, err
			return out, err
		}
		return fail(err)
	}
	out.Kind, out.Upgraded, out.InstalledVersion = OutcomeUpgraded, true, p.Target.Version.String()
	return out, nil
}

// interrupted turns a failure after the context was cancelled (SIGINT or
// SIGTERM) into the cancellation exit code. Callers use it only when nothing
// was installed or the previous executable was restored.
func interrupted(ctx context.Context, err error) error {
	if ctx.Err() == nil || errors.Is(err, errs.ErrCancelled) {
		return err
	}
	return errs.WithHint(errs.Wrap(errs.CodeCancelled, "self upgrade interrupted", ctx.Err()), "nothing was changed")
}

func parseOrZero(v string) *semver.Version {
	parsed, err := semver.NewVersion(strings.TrimPrefix(v, "v"))
	if err != nil {
		return semver.New(0, 0, 0, "", "")
	}
	return parsed
}

// apply runs the download, verification, and swap. changed reports whether
// the executable is left different from before, which only happens when a
// failed new binary could not be swapped back.
func apply(ctx context.Context, env Env, p UpgradePlan, progress func(string)) (changed bool, err error) {
	exe := p.Installation.Executable
	rel := *p.Target
	target := rel.Version.String()
	name := ArchiveName(target, env.GOOS, env.GOARCH)

	archive, ok := rel.Asset(name)
	if !ok {
		return false, errs.New(errs.CodeGeneric, fmt.Sprintf("no gskill archive for %s/%s in %s", env.GOOS, env.GOARCH, rel.Tag))
	}
	progress("Downloading " + name)
	staged, err := fetchVerified(ctx, env, exe, rel, archive, progress)
	if err != nil {
		return false, err
	}
	if err := validateStaged(ctx, env, staged, target); err != nil {
		_ = os.Remove(staged)
		return false, err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(staged)
		return false, err
	}

	backup, err := Install(exe, staged)
	if err != nil {
		_ = os.Remove(staged)
		return false, err
	}
	if got, err := env.Probe(ctx, exe); err != nil || got != target {
		if rerr := Restore(exe, backup); rerr != nil {
			return true, restoreFailed(exe, backup, got, target, rerr)
		}
		return false, installedMismatch(exe, got, target)
	}
	// The new binary is in place and verified; a leftover backup is only
	// clutter, which the next run's cleanup removes.
	if err := Discard(backup); err != nil {
		progress(fmt.Sprintf("Could not remove the backup %s: %v; delete it by hand.", backup, errors.Unwrap(err)))
	}
	return true, nil
}

// fetchVerified downloads and authenticates the manifest, then stages the
// archive's binary once its digest matches. It returns the staged path.
func fetchVerified(ctx context.Context, env Env, exe string, rel Release, archive Asset, progress func(string)) (string, error) {
	manifest, err := download(ctx, env.Source, rel, "checksums.txt")
	if err != nil {
		return "", err
	}
	bundle, err := download(ctx, env.Source, rel, "checksums.txt.sigstore.json")
	if err != nil {
		return "", err
	}
	if err := env.Verifier.VerifyManifest(ctx, manifest, bundle, rel.Tag); err != nil {
		return "", err
	}
	progress("Verified checksums.txt signature: glapsfun/gskill release.yml at " + rel.Tag)
	want, err := ChecksumFor(manifest, archive.Name)
	if err != nil {
		return "", err
	}
	staged, err := stageBinary(ctx, env, exe, archive, want)
	if err != nil {
		return "", err
	}
	progress("Verified " + archive.Name + " checksum")
	return staged, nil
}

// stageBinary downloads the archive and extracts its binary into a staged
// file next to exe, returning the staged path. The archive's digest is checked
// before this returns, so nothing unauthenticated is ever run.
func stageBinary(ctx context.Context, env Env, exe string, archive Asset, want string) (string, error) {
	info, err := os.Stat(exe)
	if err != nil {
		return "", errs.Wrap(errs.CodeGeneric, "cannot locate the running executable", err)
	}
	f, err := Stage(filepath.Dir(exe), info)
	if err != nil {
		return "", err
	}
	discard := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	rc, err := env.Source.Open(ctx, archive)
	if err != nil {
		return discard(err)
	}
	err = ExtractBinary(rc, archive.Name, want, f, maxBinaryBytes)
	_ = rc.Close()
	if err != nil {
		return discard(err)
	}
	if err := f.Sync(); err != nil {
		return discard(errs.Wrap(errs.CodeGeneric, "write the new gskill binary", err))
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", errs.Wrap(errs.CodeGeneric, "write the new gskill binary", err)
	}
	return f.Name(), nil
}

func validateStaged(ctx context.Context, env Env, staged, target string) error {
	if err := CheckPlatform(staged, env.GOOS, env.GOARCH); err != nil {
		return err
	}
	got, err := env.Probe(ctx, staged)
	if err != nil {
		return err
	}
	if got != target {
		return errs.New(errs.CodeIntegrity, fmt.Sprintf("the new binary reports version %s, want %s", got, target))
	}
	return nil
}

func installedMismatch(exe, got, target string) error {
	return errs.New(errs.CodeGeneric,
		fmt.Sprintf("the installed %s reports %s, want %s; the previous version was restored", exe, orNoVersion(got), target))
}

func restoreFailed(exe, backup, got, target string, cause error) error {
	return errs.WithHint(errs.Wrap(errs.CodeGeneric,
		fmt.Sprintf("the installed %s reports %s, want %s, and restoring the previous version failed", exe, orNoVersion(got), target), cause),
		"the previous binary is at "+backup+"; move it back to "+exe)
}

func orNoVersion(v string) string {
	if v == "" {
		return "no version"
	}
	return v
}

func download(ctx context.Context, src Source, rel Release, name string) ([]byte, error) {
	a, ok := rel.Asset(name)
	if !ok {
		return nil, errs.New(errs.CodeIntegrity, fmt.Sprintf("release %s has no %s", rel.Tag, name))
	}
	rc, err := src.Open(ctx, a)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		var coded *errs.Error
		if errors.As(err, &coded) {
			return nil, err
		}
		return nil, errs.Wrap(errs.CodeSourceUnavailable, "download "+name, err)
	}
	return data, nil
}

func baseOutcome(p UpgradePlan) Outcome {
	inst := p.Installation
	out := Outcome{
		PreviousVersion:  inst.Version,
		InstalledVersion: inst.Version,
		Executable:       inst.Executable,
		Platform:         inst.Platform(),
		InvokedAs:        inst.InvokedAs,
		Channel:          inst.Channel,
		Evidence:         inst.Evidence,
		NextCommand:      p.NextCommand,
		PathShadow:       inst.PathShadow,
	}
	if p.Target != nil {
		out.TargetVersion = p.Target.Version.String()
	}
	switch p.Action {
	case ActionNone:
		out.Kind = OutcomeUpToDate
	case ActionPrintAction:
		out.Kind = OutcomeActionRequired
	case ActionUpgrade:
		out.Kind = OutcomeDryRun
	}
	return out
}

// OutcomeFor returns the outcome a plan reports without applying it: up to
// date, action required, or, for an upgrade, a dry run.
func OutcomeFor(p UpgradePlan) Outcome { return baseOutcome(p) }
