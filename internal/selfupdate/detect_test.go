package selfupdate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
	"github.com/glapsfun/gskill/internal/version"
)

func release(v string) selfupdate.BuildInfo {
	return selfupdate.BuildInfo{Version: v, Kind: version.KindRelease, ModulePath: "github.com/glapsfun/gskill"}
}

func module(v string) selfupdate.BuildInfo {
	return selfupdate.BuildInfo{Version: v, Kind: version.KindModule, ModulePath: "github.com/glapsfun/gskill"}
}

func development(v string) selfupdate.BuildInfo {
	return selfupdate.BuildInfo{Version: v, Kind: version.KindDevelopment, ModulePath: "github.com/glapsfun/gskill"}
}

func files(m map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if body, ok := m[p]; ok {
			return []byte(body), nil
		}
		return nil, os.ErrNotExist
	}
}

type detectCase struct {
	name     string
	exe      string
	build    selfupdate.BuildInfo
	files    map[string]string
	channel  selfupdate.Channel
	evidence string
}

// detectCases are the rows of the channel-detection contract's test table,
// plus the edges around rules 4 and 6.
func detectCases() []detectCase {
	const pseudo = "v0.9.1-0.20261103120000-abcdef123456"
	projectJSON := map[string]string{"/w/app/package.json": `{"devDependencies":{"@glapsfun/gskill":"^0.9.0"}}`}
	return []detectCase{
		{
			"install script", "/usr/local/bin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelStandalone, "release build outside any package-managed location",
		},
		{
			"versioned name, not a versioned directory", "/opt/gskill-0.9.0/gskill", release("0.9.0"), nil,
			selfupdate.ChannelStandalone, "release build outside any package-managed location",
		},
		{
			"homebrew macOS", "/opt/homebrew/Caskroom/gskill/0.9.0/gskill", release("0.9.0"), nil,
			selfupdate.ChannelHomebrew, "path contains /Caskroom/gskill/",
		},
		{
			"homebrew linux", "/home/linuxbrew/.linuxbrew/Caskroom/gskill/0.9.0/gskill", release("0.9.0"), nil,
			selfupdate.ChannelHomebrew, "path contains /Caskroom/gskill/",
		},
		{
			"homebrew formula", "/usr/local/Cellar/gskill/0.9.0/bin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelHomebrew, "path contains /Cellar/gskill/",
		},
		{
			"npx", "/home/u/.npm/_npx/ab12/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelNPX, "npx cache",
		},
		{
			"global npm", "/usr/lib/node_modules/@glapsfun/gskill/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelNPMGlobal, "global npm package",
		},
		{
			"project npm", "/w/app/node_modules/@glapsfun/gskill-darwin-arm64/bin/gskill", release("0.9.0"), projectJSON,
			selfupdate.ChannelNPMProject, "npm dependency of /w/app",
		},
		{
			"pnpm layout", "/w/app/node_modules/.pnpm/x/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelNPM, "npm package (kind unknown)",
		},
		{
			"package.json without gskill", "/w/other/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", release("0.9.0"),
			map[string]string{"/w/other/package.json": `{"dependencies":{"left-pad":"1"}}`},
			selfupdate.ChannelNPM, "npm package (kind unknown)",
		},
		{
			"package.json does not parse", "/w/bad/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", release("0.9.0"),
			map[string]string{"/w/bad/package.json": `{`},
			selfupdate.ChannelNPM, "npm package (kind unknown)",
		},
		{
			"package.json over 1 MiB", "/w/big/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", release("0.9.0"),
			map[string]string{"/w/big/package.json": `{"devDependencies":{"@glapsfun/gskill":"1"},"x":"` + strings.Repeat("a", 1<<20) + `"}`},
			selfupdate.ChannelNPM, "npm package (kind unknown)",
		},
		{
			"go install tag", "/home/u/go/bin/gskill", module("v0.9.0"), nil,
			selfupdate.ChannelGo, "go install build (v0.9.0)",
		},
		{
			"go install pseudo-version", "/home/u/go/bin/gskill", module(pseudo), nil,
			selfupdate.ChannelGo, "go install build (" + pseudo + ")",
		},
		{
			"local checkout build", "/home/u/src/gskill/gskill", development(pseudo + "+dirty"), nil,
			selfupdate.ChannelDevelopment, "development build (" + pseudo + "+dirty)",
		},
		{
			"dev build on PATH", "/usr/local/bin/gskill", development("dev"), nil,
			selfupdate.ChannelDevelopment, "development build (dev)",
		},
		{
			"container", "/ko-app/gskill", release("0.9.0"), nil,
			selfupdate.ChannelContainer, "container image entrypoint /ko-app/gskill",
		},
		{
			"mise", "/home/u/.local/share/mise/installs/gskill/0.9.0/gskill", release("0.9.0"), nil,
			selfupdate.ChannelUnknown, "path is managed by another tool (/home/u/.local/share/mise/installs/gskill/0.9.0)",
		},
		{
			"aqua v-prefixed", "/home/u/.local/share/aquaproj-aqua/pkgs/github.com/glapsfun/gskill/v0.9.0/gskill", release("0.9.0"), nil,
			selfupdate.ChannelUnknown, "path is managed by another tool (/home/u/.local/share/aquaproj-aqua/pkgs/github.com/glapsfun/gskill/v0.9.0)",
		},
		{
			"nix", "/nix/store/abc-gskill-0.9.0/bin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelUnknown, "path is managed by another tool (/nix/store/abc-gskill-0.9.0)",
		},
		{
			"system package in /usr/bin", "/usr/bin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelUnknown, "path is in a system directory owned by the OS package manager (/usr/bin)",
		},
		{
			"system package in /sbin", "/sbin/gskill", release("0.9.0"), nil,
			selfupdate.ChannelUnknown, "path is in a system directory owned by the OS package manager (/sbin)",
		},
		{
			"module build of a fork", "/home/u/go/bin/gskill",
			selfupdate.BuildInfo{Version: "v1.0.0", Kind: version.KindModule, ModulePath: "example.com/fork"},
			nil,
			selfupdate.ChannelUnknown, "no evidence of how this gskill was installed",
		},
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	for _, tt := range detectCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := selfupdate.Detect(selfupdate.DetectInput{
				Executable: tt.exe,
				Build:      tt.build,
				Getenv:     func(string) string { return "" },
				ReadFile:   files(tt.files),
			})
			if got.Channel != tt.channel {
				t.Errorf("Channel = %q, want %q", got.Channel, tt.channel)
			}
			if got.Evidence != tt.evidence {
				t.Errorf("Evidence = %q, want %q", got.Evidence, tt.evidence)
			}
			if got.Executable != tt.exe || got.Version != tt.build.Version {
				t.Errorf("Installation = %+v, want executable %q and version %q", got, tt.exe, tt.build.Version)
			}
			if tt.channel == selfupdate.ChannelNPMProject && got.NPMProjectDir != "/w/app" {
				t.Errorf("NPMProjectDir = %q, want /w/app", got.NPMProjectDir)
			}
		})
	}
}

func TestResolveExecutable_Symlink(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "target", "gskill")
	link := filepath.Join(tmp, "bin", "gskill")
	for _, d := range []string{filepath.Dir(target), filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil { //nolint:gosec // executable test fixture
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}

	exe, invokedAs, err := selfupdate.ResolveExecutable(
		func() (string, error) { return link, nil }, link,
		func(string) (string, error) { return "", errors.New("unused") })
	if err != nil {
		t.Fatal(err)
	}
	if exe != want {
		t.Errorf("executable = %q, want %q", exe, want)
	}
	if invokedAs != link {
		t.Errorf("invokedAs = %q, want %q", invokedAs, link)
	}

	_, invokedAs, err = selfupdate.ResolveExecutable(
		func() (string, error) { return target, nil }, "gskill",
		func(string) (string, error) { return link, nil })
	if err != nil || invokedAs != link {
		t.Errorf("PATH lookup: invokedAs = %q, err = %v, want %q", invokedAs, err, link)
	}
}

func TestResolveExecutable_NotARegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, _, err := selfupdate.ResolveExecutable(func() (string, error) { return dir, nil }, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot locate the running executable") {
		t.Fatalf("err = %v, want cannot locate the running executable", err)
	}
	if got := errs.ExitCode(err); got != 1 {
		t.Errorf("ExitCode = %d, want 1", got)
	}
}

func TestShadow(t *testing.T) {
	t.Parallel()

	exe := filepath.Join(t.TempDir(), "gskill")
	selfupdatetest.Copy(t, selfupdatetest.BuildFake(t, "0.9.0", "ok"), exe, 0o755)
	other := selfupdatetest.BuildFake(t, "0.8.0", "ok")
	broken := selfupdatetest.BuildFake(t, "0.8.0", "fail")
	link := filepath.Join(t.TempDir(), "gskill")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		found string
		want  *selfupdate.ShadowExecutable
	}{
		{"another gskill first on PATH", other, &selfupdate.ShadowExecutable{Path: other, Version: "0.8.0"}},
		{"PATH points at this executable", link, nil},
		{"no gskill on PATH", "", nil},
		{"other gskill fails its probe", broken, &selfupdate.ShadowExecutable{Path: broken, Version: "unknown"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := selfupdatetest.Env(t, exe, release("0.9.0"), nil, selfupdatetest.Verifier{})
			env.LookPath = func(string) (string, error) {
				if tt.found == "" {
					return "", os.ErrNotExist
				}
				return tt.found, nil
			}
			got := selfupdate.Shadow(context.Background(), env, exe)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("Shadow = %+v, want nil", got)
			case tt.want != nil && (got == nil || *got != *tt.want):
				t.Errorf("Shadow = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// env maps environment variable names for a Getenv stub.
type env map[string]string

func (e env) get(k string) string { return e[k] }
