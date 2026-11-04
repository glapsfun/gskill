package selfupdate_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

// fakeSource serves releases from memory, keyed by tag.
type fakeSource struct {
	latest   string
	releases map[string]selfupdate.Release
}

func newFakeSource(latest string, tags ...string) fakeSource {
	src := fakeSource{latest: latest, releases: map[string]selfupdate.Release{}}
	for _, tag := range append(tags, latest) {
		v := semver.MustParse(strings.TrimPrefix(tag, "v"))
		src.releases[tag] = selfupdate.Release{Version: v, Tag: tag, Prerelease: v.Prerelease() != ""}
	}
	return src
}

func (f fakeSource) Latest(context.Context) (selfupdate.Release, error) {
	return f.releases[f.latest], nil
}

func (f fakeSource) ByTag(_ context.Context, tag string) (selfupdate.Release, error) {
	if rel, ok := f.releases[tag]; ok {
		return rel, nil
	}
	return selfupdate.Release{}, errs.New(errs.CodeUsage, tag+" is not a published gskill release")
}

func (fakeSource) Open(context.Context, selfupdate.Asset) (io.ReadCloser, error) {
	return nil, errs.New(errs.CodeSourceUnavailable, "fakeSource does not serve assets")
}

// layout creates an executable at <tmp>/<rel> and returns its path.
func layout(t *testing.T, rel string) string {
	t.Helper()
	exe := filepath.Join(selfupdatetest.TempDir(t), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(exe), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil { //nolint:gosec // executable test fixture
		t.Fatal(err)
	}
	return exe
}

func plan(t *testing.T, exe string, build selfupdate.BuildInfo, src selfupdate.Source, to string) (selfupdate.UpgradePlan, error) {
	t.Helper()
	env := selfupdatetest.Env(t, exe, build, src, selfupdatetest.Verifier{})
	return selfupdate.Plan(context.Background(), env, selfupdate.PlanRequest{To: to})
}

func TestPlan(t *testing.T) {
	t.Parallel()

	src := newFakeSource("v0.10.0", "v0.8.0", "v0.9.0", "v0.11.0-rc.1")
	tests := []struct {
		name    string
		current selfupdate.BuildInfo
		to      string
		action  selfupdate.Action
		target  string
		code    int
		msg     string
		hint    string
	}{
		{name: "to is not a version", current: release("0.9.0"), to: "abc", code: 2},
		{
			name: "development build", current: development("dev"), code: 1,
			msg:  "this gskill is a development build (dev) and cannot upgrade itself",
			hint: "install a release: https://github.com/glapsfun/gskill/blob/main/docs/installation.md",
		},
		{name: "older release", current: release("0.9.0"), action: selfupdate.ActionUpgrade, target: "0.10.0"},
		{name: "already latest", current: release("0.10.0"), action: selfupdate.ActionNone},
		{name: "prerelease to its stable", current: release("0.10.0-rc.1"), action: selfupdate.ActionUpgrade, target: "0.10.0"},
		{name: "prerelease ahead of stable", current: release("0.11.0-rc.1"), action: selfupdate.ActionNone},
		{name: "explicit prerelease", current: release("0.9.0"), to: "0.11.0-rc.1", action: selfupdate.ActionUpgrade, target: "0.11.0-rc.1"},
		{name: "to with v", current: release("0.9.0"), to: "v0.10.0", action: selfupdate.ActionUpgrade, target: "0.10.0"},
		{name: "to without v", current: release("0.9.0"), to: "0.10.0", action: selfupdate.ActionUpgrade, target: "0.10.0"},
		{name: "to the current version", current: release("0.9.0"), to: "0.9.0", action: selfupdate.ActionNone},
		{
			name: "downgrade", current: release("0.9.0"), to: "0.8.0", code: 2,
			msg:  "0.8.0 is older than the installed 0.9.0; downgrades are not supported",
			hint: "install an older version with the install script and VERSION=v0.8.0",
		},
		{name: "not published", current: release("0.9.0"), to: "0.42.0", code: 2, msg: "v0.42.0 is not a published gskill release"},
		{
			name: "downgrade to an unpublished version", current: release("0.9.0"), to: "0.3.1", code: 2,
			msg: "0.3.1 is older than the installed 0.9.0; downgrades are not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, err := plan(t, layout(t, "bin/gskill"), tt.current, src, tt.to)
			if got := errs.ExitCode(err); got != tt.code {
				t.Fatalf("ExitCode = %d (err %v), want %d", got, err, tt.code)
			}
			if tt.msg != "" && (err == nil || err.Error() != tt.msg) {
				t.Errorf("error = %v, want %q", err, tt.msg)
			}
			if tt.hint != "" && errs.HintOf(err) != tt.hint {
				t.Errorf("hint = %q, want %q", errs.HintOf(err), tt.hint)
			}
			if tt.code != 0 {
				return
			}
			if p.Action != tt.action {
				t.Errorf("Action = %q, want %q", p.Action, tt.action)
			}
			if tt.target != "" && (p.Target == nil || p.Target.Version.String() != tt.target) {
				t.Errorf("Target = %v, want %s", p.Target, tt.target)
			}
		})
	}
}

func TestPlan_UnwritableDirectory(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can write any directory")
	}

	exe := layout(t, "ro/gskill")
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // read-only directory under test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // restore so TempDir cleanup works

	_, err := plan(t, exe, release("0.9.0"), newFakeSource("v0.10.0"), "")
	if err == nil || err.Error() != "cannot write "+exe+": permission denied" {
		t.Fatalf("err = %v, want cannot write", err)
	}
	if errs.ExitCode(err) != 1 {
		t.Errorf("ExitCode = %d, want 1", errs.ExitCode(err))
	}
	if want := "rerun as a user who can write " + dir + ", or reinstall into a writable directory with the install script"; errs.HintOf(err) != want {
		t.Errorf("hint = %q, want %q", errs.HintOf(err), want)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the exe", len(entries))
	}
}

func TestPlan_PackageManagedPrintsAction(t *testing.T) {
	t.Parallel()

	src := newFakeSource("v0.10.0")
	for name, tt := range map[string]struct {
		path    string
		channel selfupdate.Channel
	}{
		"homebrew": {"Caskroom/gskill/0.9.0/gskill", selfupdate.ChannelHomebrew},
		"mise":     {"mise/installs/gskill/0.9.0/gskill", selfupdate.ChannelUnknown},
	} {
		p, err := plan(t, layout(t, tt.path), release("0.9.0"), src, "")
		if errs.ExitCode(err) != 8 {
			t.Errorf("%s: ExitCode = %d (err %v), want 8", name, errs.ExitCode(err), err)
		}
		if p.Action != selfupdate.ActionPrintAction || p.Installation.Channel != tt.channel {
			t.Errorf("%s: plan = %s %s, want print-action %s", name, p.Action, p.Installation.Channel, tt.channel)
		}
		if !slices.Equal(p.NextCommand, selfupdate.NextCommand(p.Installation, "")) {
			t.Errorf("%s: NextCommand = %q", name, p.NextCommand)
		}

		current, err := plan(t, layout(t, tt.path), release("0.10.0"), src, "")
		if err != nil || current.Action != selfupdate.ActionNone {
			t.Errorf("%s at the latest: action %s, err %v, want none", name, current.Action, err)
		}
	}
}

func TestNextCommand(t *testing.T) {
	t.Parallel()

	npm := []string{
		"npm install --global @glapsfun/gskill@latest",
		"update the @glapsfun/gskill dependency of your project, for example: npm install --save-dev @glapsfun/gskill@latest",
		"npx @glapsfun/gskill@latest <command>, or change the version pinned in your script",
	}
	brew := "brew upgrade glapsfun/tap/gskill"
	goInstall := "go install github.com/glapsfun/gskill/cmd/gskill@latest"
	docker := "docker pull ghcr.io/glapsfun/gskill:latest"
	tests := map[selfupdate.Channel][]string{
		selfupdate.ChannelHomebrew:   {brew},
		selfupdate.ChannelNPMGlobal:  {npm[0]},
		selfupdate.ChannelNPMProject: {"update the @glapsfun/gskill dependency of /w/app, for example: npm install --save-dev @glapsfun/gskill@latest"},
		selfupdate.ChannelNPX:        {npm[2]},
		selfupdate.ChannelNPM:        npm,
		selfupdate.ChannelGo:         {goInstall},
		selfupdate.ChannelContainer:  {docker},
		selfupdate.ChannelUnknown: append(slices.Clone(npm), brew, goInstall, docker,
			"curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh"),
	}
	for channel, want := range tests {
		inst := selfupdate.Installation{Channel: channel, NPMProjectDir: "/w/app", Executable: "/home/u/go/bin/gskill"}
		if got := selfupdate.NextCommand(inst, ""); !slices.Equal(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", channel, got, want)
		}
	}
}

func TestNextCommand_Pinned(t *testing.T) {
	t.Parallel()

	npm := []string{
		"npm install --global @glapsfun/gskill@0.10.0",
		"update the @glapsfun/gskill dependency of your project, for example: npm install --save-dev @glapsfun/gskill@0.10.0",
		"npx @glapsfun/gskill@0.10.0 <command>, or change the version pinned in your script",
	}
	brew := []string{"brew upgrade glapsfun/tap/gskill", "Homebrew installs the tap's current version, which may not be 0.10.0."}
	goInstall := "go install github.com/glapsfun/gskill/cmd/gskill@v0.10.0"
	docker := "docker pull ghcr.io/glapsfun/gskill:0.10.0"
	unknown := append(append(slices.Clone(npm), brew...), goInstall, docker,
		"curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | VERSION=v0.10.0 sh")
	tests := map[selfupdate.Channel][]string{
		selfupdate.ChannelHomebrew:   brew,
		selfupdate.ChannelNPMGlobal:  {npm[0]},
		selfupdate.ChannelNPMProject: {"update the @glapsfun/gskill dependency of /w/app, for example: npm install --save-dev @glapsfun/gskill@0.10.0"},
		selfupdate.ChannelNPX:        {npm[2]},
		selfupdate.ChannelNPM:        npm,
		selfupdate.ChannelGo:         {goInstall},
		selfupdate.ChannelContainer:  {docker},
		selfupdate.ChannelUnknown:    unknown,
	}
	for channel, want := range tests {
		inst := selfupdate.Installation{Channel: channel, NPMProjectDir: "/w/app", Executable: "/home/u/go/bin/gskill", GoInstallDir: "/home/u/go/bin"}
		if got := selfupdate.NextCommand(inst, "0.10.0"); !slices.Equal(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", channel, got, want)
		}
	}
}

func TestNextCommand_GoInstallDirNote(t *testing.T) {
	t.Parallel()

	inst := selfupdate.Detect(selfupdate.DetectInput{
		Executable: "/opt/tools/gskill",
		Build:      module("v0.9.0"),
		Getenv:     env{"HOME": "/home/u"}.get,
	})
	note := "`go install` writes to /home/u/go/bin, not to /opt/tools/gskill"
	for _, pin := range []string{"", "0.10.0"} {
		got := selfupdate.NextCommand(inst, pin)
		if len(got) != 2 || got[1] != note {
			t.Errorf("pin %q: NextCommand = %q, want the go install line and %q", pin, got, note)
		}
	}

	inGOBIN := selfupdate.Detect(selfupdate.DetectInput{
		Executable: "/home/u/bin/gskill",
		Build:      module("v0.9.0"),
		Getenv:     env{"HOME": "/home/u", "GOBIN": "/home/u/bin"}.get,
	})
	if got := selfupdate.NextCommand(inGOBIN, ""); len(got) != 1 {
		t.Errorf("exe in GOBIN: NextCommand = %q, want no note", got)
	}
}

func TestPlan_PinnedPrintAction(t *testing.T) {
	t.Parallel()

	p, err := plan(t, layout(t, "lib/node_modules/@glapsfun/gskill/node_modules/@glapsfun/gskill-linux-x64/bin/gskill"),
		release("0.9.0"), newFakeSource("v0.11.0", "v0.10.0"), "0.10.0")
	if errs.ExitCode(err) != 8 {
		t.Fatalf("ExitCode = %d (err %v), want 8", errs.ExitCode(err), err)
	}
	if want := []string{"npm install --global @glapsfun/gskill@0.10.0"}; !slices.Equal(p.NextCommand, want) {
		t.Errorf("NextCommand = %q, want %q", p.NextCommand, want)
	}
}

// A development build is refused before anything else runs, including the
// other gskill on PATH that the shadow report would execute.
func TestPlan_DevelopmentBuildRunsNoOtherGskill(t *testing.T) {
	t.Parallel()

	exe := layout(t, "bin/gskill")
	env := selfupdatetest.Env(t, exe, development("dev"), newFakeSource("v0.10.0"), selfupdatetest.Verifier{})
	looked := false
	env.LookPath = func(string) (string, error) {
		looked = true
		return "", os.ErrNotExist
	}
	if _, err := selfupdate.Plan(context.Background(), env, selfupdate.PlanRequest{}); errs.ExitCode(err) != 1 {
		t.Fatalf("err = %v, want the development-build refusal", err)
	}
	if looked {
		t.Error("Plan looked up another gskill on PATH for a development build")
	}
}
