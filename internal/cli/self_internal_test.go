package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
	"github.com/glapsfun/gskill/internal/version"
)

const platform = runtime.GOOS + "/" + runtime.GOARCH

// selfFixture installs a fake gskill 0.9.0 at <tmp>/<rel> and serves v0.10.0.
type selfFixture struct {
	exe string
	srv *selfupdatetest.Server
	app *app.App
}

func newSelfFixture(t *testing.T, rel, current string) selfFixture {
	t.Helper()
	return newSelfFixtureWith(t, rel, current, nil)
}

// newSelfFixtureWith lets a test adjust the environment, such as PATH lookups.
func newSelfFixtureWith(t *testing.T, rel, current string, adjust func(*selfupdate.Env)) selfFixture {
	t.Helper()
	exe := filepath.Join(selfupdatetest.TempDir(t), filepath.FromSlash(rel))
	selfupdatetest.Copy(t, selfupdatetest.BuildFake(t, current, "ok"), exe, 0o755)
	srv := selfupdatetest.NewServer(t, selfupdatetest.ServerOptions{
		Releases: []selfupdatetest.ReleaseSpec{selfupdatetest.Release(t, "0.10.0")},
	})
	build := selfupdate.BuildInfo{Version: current, Kind: version.KindRelease, ModulePath: "github.com/glapsfun/gskill"}
	env := selfupdatetest.Env(t, exe, build, selfupdate.NewGitHubSource(srv.Client(), srv.APIBase(), ""), selfupdatetest.Verifier{})
	if adjust != nil {
		adjust(&env)
	}
	return selfFixture{exe: exe, srv: srv, app: app.New(app.Options{SelfUpdate: &env})}
}

// run executes `self upgrade` through the same error reporting as Run.
func (f selfFixture) run(t *testing.T, out *Output, cmd selfUpgradeCmd, g Globals) int {
	t.Helper()
	if err := cmd.Run(context.Background(), out, f.app, g); err != nil {
		return reportRunError(out, err)
	}
	return 0
}

func (f selfFixture) bytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(f.exe)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSelfUpgrade_InteractiveYes(t *testing.T) {
	t.Parallel()

	f := newSelfFixture(t, "bin/gskill", "0.9.0")
	var stdout, stderr bytes.Buffer
	code := f.run(t, newInteractiveOutput(&stdout, &stderr, "y\n"), selfUpgradeCmd{}, Globals{})
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "Upgraded gskill from 0.9.0 to 0.10.0 at "+f.exe {
		t.Errorf("stdout = %q", got)
	}
	for _, want := range []string{
		"gskill 0.9.0 (standalone, " + platform + ") at " + f.exe,
		"Latest stable release: 0.10.0",
		"Upgrade " + f.exe + " from 0.9.0 to 0.10.0?",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr.String())
		}
	}
}

func TestSelfUpgrade_InteractiveNo(t *testing.T) {
	t.Parallel()

	f := newSelfFixture(t, "bin/gskill", "0.9.0")
	before := f.bytes(t)
	var stdout, stderr bytes.Buffer
	code := f.run(t, newInteractiveOutput(&stdout, &stderr, "n\n"), selfUpgradeCmd{}, Globals{})
	if code != 1 || !strings.Contains(stderr.String(), "aborted") {
		t.Errorf("exit %d, stderr %q, want 1 and aborted", code, stderr.String())
	}
	if !bytes.Equal(f.bytes(t), before) {
		t.Error("exe changed after declining")
	}
	if f.srv.AnyAssetRequested() {
		t.Errorf("assets requested after declining: %q", f.srv.Requests())
	}
}

func TestSelfUpgrade_NonInteractiveNeedsYes(t *testing.T) {
	t.Parallel()

	f := newSelfFixture(t, "bin/gskill", "0.9.0")
	var stdout, stderr bytes.Buffer
	out := NewOutput(&stdout, &stderr, OutputOptions{Interactive: false})
	code := f.run(t, out, selfUpgradeCmd{}, Globals{})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	for _, want := range []string{"self upgrade requires confirmation", "pass --yes to upgrade without a prompt"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, stderr.String())
		}
	}
	if f.srv.AnyAssetRequested() {
		t.Errorf("assets requested without --yes: %q", f.srv.Requests())
	}
}

func TestSelfUpgrade_YesNonInteractive(t *testing.T) {
	t.Parallel()

	f := newSelfFixture(t, "bin/gskill", "0.9.0")
	var stdout, stderr bytes.Buffer
	out := NewOutput(&stdout, &stderr, OutputOptions{Interactive: false})
	out.stdin = failingReader{t}
	if code := f.run(t, out, selfUpgradeCmd{}, Globals{Yes: true}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "Upgraded gskill from 0.9.0 to 0.10.0") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestSelfUpgrade_UpToDate(t *testing.T) {
	t.Parallel()

	f := newSelfFixture(t, "bin/gskill", "0.10.0")
	var stdout, stderr bytes.Buffer
	if code := f.run(t, NewOutput(&stdout, &stderr, OutputOptions{}), selfUpgradeCmd{}, Globals{}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	want := "gskill 0.10.0 is up to date (standalone, " + platform + ", " + f.exe + ")"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestSelfUpgrade_HomebrewPrintsCommand(t *testing.T) {
	t.Parallel()

	f := newSelfFixture(t, "Caskroom/gskill/0.9.0/gskill", "0.9.0")
	before := f.bytes(t)
	var stdout, stderr bytes.Buffer
	code := f.run(t, NewOutput(&stdout, &stderr, OutputOptions{}), selfUpgradeCmd{}, Globals{Yes: true})
	if code != 8 {
		t.Errorf("exit %d, want 8", code)
	}
	want := "gskill 0.9.0 (" + platform + ") is managed by Homebrew (" + f.exe + ")\n" +
		"gskill 0.10.0 is available. No upgrade was performed. To upgrade, run:\n" +
		"  brew upgrade glapsfun/tap/gskill\n"
	if stdout.String() != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout.String(), want)
	}
	if strings.Contains(stderr.String(), "error:") {
		t.Errorf("stderr has an error line: %q", stderr.String())
	}
	if !bytes.Equal(f.bytes(t), before) || f.srv.AnyAssetRequested() {
		t.Error("a Homebrew-owned binary was touched or an asset was downloaded")
	}
}

// failingReader fails the test if anything reads stdin.
type failingReader struct{ t *testing.T }

func (r failingReader) Read([]byte) (int, error) {
	r.t.Error("stdin was read in a non-interactive run")
	return 0, os.ErrClosed
}

func TestSelfUpgrade_NamesAnotherGskillOnPath(t *testing.T) {
	t.Parallel()

	other := selfupdatetest.BuildFake(t, "0.8.0", "ok")
	f := newSelfFixtureWith(t, "bin/gskill", "0.9.0", func(env *selfupdate.Env) {
		env.LookPath = func(string) (string, error) { return other, nil }
	})
	var stdout, stderr bytes.Buffer
	if code := f.run(t, newInteractiveOutput(&stdout, &stderr, "y\n"), selfUpgradeCmd{}, Globals{}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	note := "Note: `gskill` on PATH is " + other + " (0.8.0), not this executable. It was not changed."
	if got := strings.Count(stderr.String(), note); got != 2 {
		t.Errorf("note appears %d times, want before the prompt and after the result:\n%s", got, stderr.String())
	}
	prompt := strings.Index(stderr.String(), "Upgrade "+f.exe)
	if first := strings.Index(stderr.String(), note); first < 0 || first > prompt {
		t.Errorf("the first note must come before the prompt:\n%s", stderr.String())
	}
}

func TestSelfUpgrade_PinnedNPMCommand(t *testing.T) {
	t.Parallel()

	f := newSelfFixture(t, "lib/node_modules/@glapsfun/gskill/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", "0.9.0")
	var stdout, stderr bytes.Buffer
	code := f.run(t, NewOutput(&stdout, &stderr, OutputOptions{}), selfUpgradeCmd{To: "0.10.0"}, Globals{Yes: true})
	if code != 8 {
		t.Errorf("exit %d, want 8", code)
	}
	want := "gskill 0.9.0 (" + platform + ") is managed by npm (" + f.exe + ")\n" +
		"gskill 0.10.0 is available. No upgrade was performed. To upgrade, run:\n" +
		"  npm install --global @glapsfun/gskill@0.10.0\n"
	if stdout.String() != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout.String(), want)
	}
}

// #1: when another upgrade finished while this one waited for the lock, the
// output says up to date, not upgraded.
func TestSelfUpgrade_AnotherRunFinishedFirst(t *testing.T) {
	t.Parallel()

	var exe string
	f := newSelfFixtureWith(t, "bin/gskill", "0.9.0", func(env *selfupdate.Env) {
		exe, _ = env.Executable()
		env.Probe = func(context.Context, string) (string, error) { return "0.10.0", nil }
	})
	var stdout, stderr bytes.Buffer
	if code := f.run(t, NewOutput(&stdout, &stderr, OutputOptions{}), selfUpgradeCmd{}, Globals{Yes: true}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	want := "gskill 0.10.0 is up to date (standalone, " + platform + ", " + exe + ")"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if f.srv.AnyAssetRequested() {
		t.Error("an asset was downloaded although the executable was already current")
	}
}
