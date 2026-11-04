package selfupdate_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

// upgradeFixture is a standalone gskill 0.9.0 in a temp dir and a fake server
// publishing v0.10.0 with a valid archive and manifest.
type upgradeFixture struct {
	exe     string
	dir     string
	srv     *selfupdatetest.Server
	archive string
	env     selfupdate.Env
}

func newUpgradeFixture(t *testing.T, opts selfupdatetest.ServerOptions, archiveBinary []byte) upgradeFixture {
	t.Helper()
	dir := filepath.Join(selfupdatetest.TempDir(t), "bin")
	exe := filepath.Join(dir, "gskill")
	selfupdatetest.Copy(t, selfupdatetest.BuildFake(t, "0.9.0", "ok"), exe, 0o755)

	if archiveBinary == nil {
		var err error
		archiveBinary, err = os.ReadFile(selfupdatetest.BuildFake(t, newVersion, "ok"))
		if err != nil {
			t.Fatal(err)
		}
	}
	name := selfupdate.ArchiveName(newVersion, runtime.GOOS, runtime.GOARCH)
	archive := selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Body: archiveBinary})
	if len(opts.Releases) == 0 {
		opts.Releases = []selfupdatetest.ReleaseSpec{{Tag: "v0.10.0", Assets: map[string][]byte{
			name:                          archive,
			"checksums.txt":               selfupdatetest.Checksums(map[string][]byte{name: archive}),
			"checksums.txt.sigstore.json": []byte("{}"),
		}}}
	}
	srv := selfupdatetest.NewServer(t, opts)
	src := selfupdate.NewGitHubSource(srv.Client(), srv.APIBase(), "")
	env := selfupdatetest.Env(t, exe, release("0.9.0"), src, selfupdatetest.Verifier{})
	return upgradeFixture{exe: exe, dir: dir, srv: srv, archive: name, env: env}
}

// run plans and applies the upgrade. A Plan error is returned as is.
func (f upgradeFixture) run(ctx context.Context) (selfupdate.Outcome, []string, error) {
	p, err := selfupdate.Plan(ctx, f.env, selfupdate.PlanRequest{})
	if err != nil {
		return selfupdate.Outcome{}, nil, err
	}
	var progress []string
	out, err := selfupdate.Apply(ctx, f.env, p, func(line string) { progress = append(progress, line) })
	return out, progress, err
}

func reportedVersion(t *testing.T, exe string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), exe, "version", "--json").Output()
	if err != nil {
		t.Fatalf("run %s: %v", exe, err)
	}
	return string(out)
}

const newVersion = "0.10.0"

func TestApplyUpgrades(t *testing.T) {
	t.Parallel()

	f := newUpgradeFixture(t, selfupdatetest.ServerOptions{}, nil)
	out, progress, err := f.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertUpgraded(t, out)
	if got := reportedVersion(t, f.exe); !strings.Contains(got, `"version":"`+newVersion+`"`) {
		t.Errorf("installed exe reports %s, want %s", got, newVersion)
	}
	if info, err := os.Stat(f.exe); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("exe mode = %v (err %v), want 0755", info.Mode().Perm(), err)
	}
	entries, _ := os.ReadDir(f.dir)
	if len(entries) != 1 {
		t.Errorf("%s holds %d entries, want only gskill", f.dir, len(entries))
	}
	want := []string{
		"Downloading " + f.archive,
		"Verified checksums.txt signature: glapsfun/gskill release.yml at v0.10.0",
		"Verified " + f.archive + " checksum",
	}
	if !slices.Equal(progress, want) {
		t.Errorf("progress = %q, want %q", progress, want)
	}
}

func assertUpgraded(t *testing.T, out selfupdate.Outcome) {
	t.Helper()
	if out.Kind != selfupdate.OutcomeUpgraded || !out.Upgraded || out.PreviousVersion != "0.9.0" ||
		out.TargetVersion != newVersion || out.InstalledVersion != newVersion {
		t.Errorf("outcome = %+v", out)
	}
	if out.Platform != runtime.GOOS+"/"+runtime.GOARCH || out.Channel != selfupdate.ChannelStandalone {
		t.Errorf("outcome platform/channel = %s %s", out.Platform, out.Channel)
	}
}

// spec builds a v0.10.0 release around bin, then lets mutate break it.
func spec(t *testing.T, bin []byte, mutate func(name string, assets map[string][]byte)) selfupdatetest.ReleaseSpec {
	t.Helper()
	name := selfupdate.ArchiveName(newVersion, runtime.GOOS, runtime.GOARCH)
	archive := selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Body: bin})
	assets := map[string][]byte{
		name:                          archive,
		"checksums.txt":               selfupdatetest.Checksums(map[string][]byte{name: archive}),
		"checksums.txt.sigstore.json": []byte("{}"),
	}
	if mutate != nil {
		mutate(name, assets)
	}
	return selfupdatetest.ReleaseSpec{Tag: "v" + newVersion, Assets: assets}
}

func readBin(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-built fake
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type failCase struct {
	name   string
	opts   func(t *testing.T, good []byte) selfupdatetest.ServerOptions
	env    func(env *selfupdate.Env, cancel context.CancelFunc)
	cancel func(f upgradeFixture, cancel context.CancelFunc)
	code   int
	msg    string
	hint   string
}

func failCases() []failCase {
	withRelease := func(mutate func(string, map[string][]byte)) func(*testing.T, []byte) selfupdatetest.ServerOptions {
		return func(t *testing.T, good []byte) selfupdatetest.ServerOptions {
			t.Helper()
			return selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{spec(t, good, mutate)}}
		}
	}
	withBinary := func(bin func(t *testing.T) []byte) func(*testing.T, []byte) selfupdatetest.ServerOptions {
		return func(t *testing.T, _ []byte) selfupdatetest.ServerOptions {
			t.Helper()
			return selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{spec(t, bin(t), nil)}}
		}
	}
	otherArch := map[string]string{"amd64": "arm64", "arm64": "amd64"}[runtime.GOARCH]
	return []failCase{
		{
			name: "signature does not verify", code: 6, opts: withRelease(nil),
			env: func(env *selfupdate.Env, _ context.CancelFunc) {
				env.Verifier = selfupdatetest.Verifier{Err: errs.New(errs.CodeIntegrity, "checksums.txt for v0.10.0 is not signed by the gskill release workflow")}
			},
		},
		{name: "archive digest differs", code: 6, opts: withRelease(func(name string, a map[string][]byte) {
			a["checksums.txt"] = []byte(strings.Repeat("0", 64) + "  " + name + "\n")
		})},
		{
			name: "no archive for this platform", code: 1, msg: "no gskill archive for " + runtime.GOOS + "/" + runtime.GOARCH + " in v0.10.0",
			opts: withRelease(func(name string, a map[string][]byte) { delete(a, name) }),
		},
		{name: "checksums.txt missing", code: 6, opts: withRelease(func(_ string, a map[string][]byte) { delete(a, "checksums.txt") })},
		{name: "bundle missing", code: 6, opts: withRelease(func(_ string, a map[string][]byte) { delete(a, "checksums.txt.sigstore.json") })},
		{name: "truncated archive", code: 6, opts: withRelease(func(name string, a map[string][]byte) {
			a[name] = a[name][:len(a[name])/2]
			a["checksums.txt"] = selfupdatetest.Checksums(map[string][]byte{name: a[name]})
		})},
		{name: "foreign platform binary", code: 6, opts: withBinary(func(t *testing.T) []byte {
			t.Helper()
			return readBin(t, selfupdatetest.BuildFakeFor(t, runtime.GOOS, otherArch, newVersion))
		})},
		{
			name: "binary reports another version", code: 6, msg: "the new binary reports version 0.11.0, want 0.10.0",
			opts: withBinary(func(t *testing.T) []byte {
				t.Helper()
				return readBin(t, selfupdatetest.BuildFake(t, "0.11.0", "ok"))
			}),
		},
		{name: "connection drops mid-archive", code: 5, opts: func(t *testing.T, good []byte) selfupdatetest.ServerOptions {
			t.Helper()
			name := selfupdate.ArchiveName(newVersion, runtime.GOOS, runtime.GOARCH)
			return selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{spec(t, good, nil)}, DropAfter: map[string]int{name: 1024}}
		}},
		{name: "metadata 500", code: 5, opts: func(t *testing.T, good []byte) selfupdatetest.ServerOptions {
			t.Helper()
			return selfupdatetest.ServerOptions{
				Releases: []selfupdatetest.ReleaseSpec{spec(t, good, nil)},
				Status:   map[string]int{"/repos/glapsfun/gskill/releases/latest": 500},
			}
		}},
		{
			name: "rate limited", code: 5, msg: "GitHub API rate limit reached while reading gskill releases", hint: "wait and retry, or set GITHUB_TOKEN",
			opts: func(t *testing.T, good []byte) selfupdatetest.ServerOptions {
				t.Helper()
				return selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{spec(t, good, nil)}, RateLimited: true}
			},
		},
		{
			name: "installed binary fails its check", code: 1, opts: withRelease(nil),
			env: func(env *selfupdate.Env, _ context.CancelFunc) {
				// The executable keeps reporting the old version once the new
				// binary has passed its own check.
				probe := env.Probe
				exe, _ := env.Executable()
				if resolved, err := filepath.EvalSymlinks(exe); err == nil {
					exe = resolved
				}
				stagedChecked := false
				env.Probe = func(ctx context.Context, path string) (string, error) {
					if path != exe {
						stagedChecked = true
					} else if stagedChecked {
						return "0.9.0", nil
					}
					return probe(ctx, path)
				}
			},
		},
		{name: "cancelled during download", code: 130, opts: func(t *testing.T, good []byte) selfupdatetest.ServerOptions {
			t.Helper()
			name := selfupdate.ArchiveName(newVersion, runtime.GOOS, runtime.GOARCH)
			return selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{spec(t, good, nil)}, Block: map[string]chan struct{}{name: make(chan struct{})}}
		}, cancel: func(f upgradeFixture, cancel context.CancelFunc) {
			for !f.srv.AssetRequested(f.archive) {
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
		}},
		{
			name: "cancelled after the swap", code: 130, hint: "nothing was changed", opts: withRelease(nil),
			env: func(env *selfupdate.Env, cancel context.CancelFunc) {
				afterSwap(env, func(context.Context) (string, error) {
					cancel()
					return "", context.Canceled
				})
			},
		},
		{
			name: "cancelled after staging", code: 130, opts: withRelease(nil),
			env: func(env *selfupdate.Env, cancel context.CancelFunc) {
				env.Probe = func(context.Context, string) (string, error) {
					cancel()
					return "", context.Canceled
				}
			},
		},
	}
}

func TestApplyFailuresKeepExecutable(t *testing.T) {
	t.Parallel()

	for _, tc := range failCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			good := readBin(t, selfupdatetest.BuildFake(t, newVersion, "ok"))
			f := newUpgradeFixture(t, tc.opts(t, good), good)
			original := readBin(t, f.exe)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.env != nil {
				tc.env(&f.env, cancel)
			}
			if tc.cancel != nil {
				go tc.cancel(f, cancel)
			}

			out, _, err := f.run(ctx)
			if got := errs.ExitCode(err); got != tc.code {
				t.Fatalf("ExitCode = %d (err %v), want %d", got, err, tc.code)
			}
			if tc.msg != "" && err.Error() != tc.msg {
				t.Errorf("error = %q, want %q", err, tc.msg)
			}
			if tc.hint != "" && errs.HintOf(err) != tc.hint {
				t.Errorf("hint = %q, want %q", errs.HintOf(err), tc.hint)
			}
			if out.Kind != "" && out.Kind != selfupdate.OutcomeFailed {
				t.Errorf("Outcome.Kind = %q, want failed", out.Kind)
			}
			if !bytes.Equal(readBin(t, f.exe), original) {
				t.Error("the executable changed after a failed upgrade")
			}
			assertNoLeftovers(t, f.dir)
		})
	}
}

func TestApply_UnwritableDirectoryDownloadsNothing(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can write any directory")
	}

	f := newUpgradeFixture(t, selfupdatetest.ServerOptions{}, nil)
	if err := os.Chmod(f.dir, 0o555); err != nil { //nolint:gosec // read-only directory under test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.dir, 0o755) }) //nolint:gosec // restore so TempDir cleanup works
	_, _, err := f.run(context.Background())
	if errs.ExitCode(err) != 1 || f.srv.AnyAssetRequested() {
		t.Errorf("err = %v, assets requested %v; want exit 1 and no download", err, f.srv.AnyAssetRequested())
	}
}

// afterSwap makes the executable's post-install check return what check
// returns, once the staged binary has passed its own check.
func afterSwap(env *selfupdate.Env, check func(context.Context) (string, error)) {
	probe := env.Probe
	exe, _ := env.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	stagedChecked := false
	env.Probe = func(ctx context.Context, path string) (string, error) {
		if path != exe {
			stagedChecked = true
		} else if stagedChecked {
			return check(ctx)
		}
		return probe(ctx, path)
	}
}

// #4: a lock that cannot be taken for another reason is not reported as a
// concurrent upgrade.
func TestApply_LockErrorKeepsItsCause(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can open any directory")
	}

	f := newUpgradeFixture(t, selfupdatetest.ServerOptions{}, nil)
	p, err := selfupdate.Plan(context.Background(), f.env, selfupdate.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Write and search permission, but no read: the directory cannot be opened.
	if err := os.Chmod(f.dir, 0o311); err != nil { //nolint:gosec // unreadable directory under test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.dir, 0o755) }) //nolint:gosec // restore so TempDir cleanup works
	_, err = selfupdate.Apply(context.Background(), f.env, p, nil)
	if errs.ExitCode(err) != 12 {
		t.Fatalf("err = %v, want exit 12", err)
	}
	if strings.Contains(err.Error(), "in progress") || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %q, want the real cause, not a concurrent upgrade", err)
	}
}

// #2: a verified upgrade whose backup cannot be removed still succeeds.
//
//nolint:paralleltest // replaces a package-level file operation
func TestApply_BackupRemovalFailureStillSucceeds(t *testing.T) {
	defer selfupdate.SetRemoveBackup(func(string) error { return os.ErrPermission })()

	f := newUpgradeFixture(t, selfupdatetest.ServerOptions{}, nil)
	out, progress, err := f.run(context.Background())
	if err != nil {
		t.Fatalf("err = %v, want success", err)
	}
	assertUpgraded(t, out)
	last := progress[len(progress)-1]
	if !strings.HasPrefix(last, "Could not remove the backup "+f.dir+"/.gskill-previous-") || !strings.HasSuffix(last, "; delete it by hand.") {
		t.Errorf("last progress line = %q, want the backup warning", last)
	}
}

// #6: when the new binary fails its check and the old one cannot be put back,
// the error says so and names the backup.
//
//nolint:paralleltest // replaces a package-level file operation
func TestApply_RestoreFailureNamesTheBackup(t *testing.T) {
	defer selfupdate.SetRestoreRename(func(string, string) error { return os.ErrPermission })()

	f := newUpgradeFixture(t, selfupdatetest.ServerOptions{}, nil)
	afterSwap(&f.env, func(context.Context) (string, error) { return "0.9.0", nil })
	_, _, err := f.run(context.Background())
	assertRestoreFailed(t, f, err)
}

// #7: an interrupt during the post-install check, when restoring fails, keeps
// the restore failure instead of claiming nothing changed.
//
//nolint:paralleltest // replaces a package-level file operation
func TestApply_InterruptWithFailedRestoreKeepsTheCause(t *testing.T) {
	defer selfupdate.SetRestoreRename(func(string, string) error { return os.ErrPermission })()

	f := newUpgradeFixture(t, selfupdatetest.ServerOptions{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	afterSwap(&f.env, func(context.Context) (string, error) {
		cancel()
		return "", context.Canceled
	})
	_, _, err := f.run(ctx)
	assertRestoreFailed(t, f, err)
}

func assertRestoreFailed(t *testing.T, f upgradeFixture, err error) {
	t.Helper()
	if errs.ExitCode(err) != 1 {
		t.Fatalf("ExitCode = %d (err %v), want 1", errs.ExitCode(err), err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "restoring the previous version failed") || strings.Contains(msg, "was restored") {
		t.Errorf("error = %q, want it to say the restore failed", msg)
	}
	hint := errs.HintOf(err)
	if !strings.HasPrefix(hint, "the previous binary is at "+f.dir+"/.gskill-previous-") || !strings.HasSuffix(hint, "; move it back to "+f.exe) {
		t.Errorf("hint = %q, want the backup path and the executable", hint)
	}
}
