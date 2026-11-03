package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
	"github.com/glapsfun/gskill/internal/version"
)

// selfUpgradeApp returns an App whose self-upgrade works on a fake gskill
// <current> at <tmp>/<rel>, against a fake server publishing the releases.
func selfUpgradeApp(t *testing.T, rel, current string, opts selfupdatetest.ServerOptions) (*app.App, string, *selfupdatetest.Server) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), filepath.FromSlash(rel))
	selfupdatetest.Copy(t, selfupdatetest.BuildFake(t, current, "ok"), exe, 0o755)
	if len(opts.Releases) == 0 {
		opts.Releases = []selfupdatetest.ReleaseSpec{selfupdatetest.Release(t, "0.10.0")}
	}
	srv := selfupdatetest.NewServer(t, opts)
	build := selfupdate.BuildInfo{Version: current, Kind: version.KindRelease, ModulePath: "github.com/glapsfun/gskill"}
	env := selfupdatetest.Env(t, exe, build, selfupdate.NewGitHubSource(srv.Client(), srv.APIBase(), ""), selfupdatetest.Verifier{})
	a := app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: filepath.Join(t.TempDir(), "gskill-home"),
		SelfUpdate: &env,
	})
	return a, exe, srv
}

// treeHashes maps every file and symlink under root to its content hash or
// link target.
func treeHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[rel] = "link:" + target
		case d.Type().IsRegular():
			data, err := os.ReadFile(p) //nolint:gosec // walking the test's own temp tree
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			out[rel] = hex.EncodeToString(sum[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSelfUpgrade_LeavesProjectUntouched(t *testing.T) {
	t.Parallel()

	a, exe, _ := selfUpgradeApp(t, "bin/gskill", "0.9.0", selfupdatetest.ServerOptions{})
	proj := newProject(t)
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", localSkillDir(t, "demo"), "--agent", "claude"); code != 0 {
		t.Fatalf("add: exit %d: %s", code, stderr)
	}
	projectBefore := treeHashes(t, proj)
	homeBefore := treeHashes(t, a.GskillHome())
	if len(projectBefore) == 0 || len(homeBefore) == 0 {
		t.Fatal("fixture project or home is empty")
	}

	stdout, stderr, code := runGskillWithApp(t, a, proj, "--yes", "self", "upgrade")
	if code != 0 {
		t.Fatalf("self upgrade: exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Upgraded gskill from 0.9.0 to 0.10.0 at "+exe) {
		t.Errorf("stdout = %q", stdout)
	}
	if after := treeHashes(t, proj); !maps.Equal(projectBefore, after) {
		t.Errorf("project changed:\nbefore %v\nafter  %v", projectBefore, after)
	}
	if after := treeHashes(t, a.GskillHome()); !maps.Equal(homeBefore, after) {
		t.Errorf("gskill home changed:\nbefore %v\nafter  %v", homeBefore, after)
	}
}

func TestSelfUpgrade_OutsideProject(t *testing.T) {
	t.Parallel()

	a, _, _ := selfUpgradeApp(t, "bin/gskill", "0.9.0", selfupdatetest.ServerOptions{})
	if _, stderr, code := runGskillWithApp(t, a, t.TempDir(), "--yes", "self", "upgrade"); code != 0 {
		t.Fatalf("self upgrade outside a project: exit %d: %s", code, stderr)
	}
}

func TestSelfUpgrade_PackageManagedIsNotWritten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, rel, manager, command string
	}{
		{"homebrew", "Caskroom/gskill/0.9.0/gskill", "Homebrew", "brew upgrade glapsfun/tap/gskill"},
		{"global npm", "lib/node_modules/@glapsfun/gskill/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", "npm", "npm install --global @glapsfun/gskill@latest"},
		{"npx", "_npx/ab12/node_modules/@glapsfun/gskill-linux-x64/bin/gskill", "npx", "npx @glapsfun/gskill@latest <command>, or change the version pinned in your script"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, exe, srv := selfUpgradeApp(t, tt.rel, "0.9.0", selfupdatetest.ServerOptions{})
			before := treeHashes(t, filepath.Dir(exe))
			stdout, stderr, code := runGskillWithApp(t, a, t.TempDir(), "--yes", "self", "upgrade")
			if code != 8 {
				t.Errorf("exit %d, want 8 (stderr %q)", code, stderr)
			}
			want := "gskill 0.9.0 (" + runtime.GOOS + "/" + runtime.GOARCH + ") is managed by " + tt.manager + " (" + exe + ")\n" +
				"gskill 0.10.0 is available. No upgrade was performed. To upgrade, run:\n" +
				"  " + tt.command + "\n"
			if stdout != want {
				t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
			}
			if strings.Contains(stderr, "error:") {
				t.Errorf("stderr has an error line: %q", stderr)
			}
			if after := treeHashes(t, filepath.Dir(exe)); !maps.Equal(before, after) {
				t.Error("files owned by the package manager changed")
			}
			if srv.AnyAssetRequested() {
				t.Errorf("assets downloaded for a package-managed install: %q", srv.Requests())
			}
		})
	}
}

func TestSelfUpgrade_DryRun(t *testing.T) {
	t.Parallel()

	a, exe, srv := selfUpgradeApp(t, "bin/gskill", "0.9.0", selfupdatetest.ServerOptions{})
	before := treeHashes(t, filepath.Dir(exe))
	stdout, stderr, code := runGskillWithApp(t, a, t.TempDir(), "--dry-run", "self", "upgrade")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	want := "Dry run: would upgrade " + exe + " from 0.9.0 to 0.10.0 (standalone). Nothing was downloaded or changed.\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if srv.AnyAssetRequested() {
		t.Errorf("dry run downloaded assets: %q", srv.Requests())
	}
	if after := treeHashes(t, filepath.Dir(exe)); !maps.Equal(before, after) {
		t.Error("dry run changed the install directory")
	}

	brew, _, _ := selfUpgradeApp(t, "Caskroom/gskill/0.9.0/gskill", "0.9.0", selfupdatetest.ServerOptions{})
	stdout, _, code = runGskillWithApp(t, brew, t.TempDir(), "--dry-run", "--json", "self", "upgrade")
	if code != 8 || jsonOutcome(t, stdout) != "action_required" {
		t.Errorf("Homebrew dry run: exit %d, stdout %s; want 8 and action_required", code, stdout)
	}
}

func TestSelfUpgrade_DryRunReportsPermission(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can write any directory")
	}

	a, exe, _ := selfUpgradeApp(t, "ro/gskill", "0.9.0", selfupdatetest.ServerOptions{})
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // read-only directory under test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // restore so TempDir cleanup works
	_, stderr, code := runGskillWithApp(t, a, t.TempDir(), "--dry-run", "self", "upgrade")
	if code != 1 || !strings.Contains(stderr, "cannot write "+exe+": permission denied") {
		t.Errorf("exit %d, stderr %q; want 1 and cannot write", code, stderr)
	}
}

func TestSelfUpgrade_OfflineRefusesBeforeNetwork(t *testing.T) {
	t.Parallel()

	a, _, srv := selfUpgradeApp(t, "bin/gskill", "0.9.0", selfupdatetest.ServerOptions{})
	_, stderr, code := runGskillWithApp(t, a, t.TempDir(), "--offline", "self", "upgrade")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	for _, want := range []string{"self upgrade needs the network to find releases", "drop --offline"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q: %q", want, stderr)
		}
	}
	if got := srv.Requests(); len(got) != 0 {
		t.Errorf("offline run made requests: %q", got)
	}
}

// jsonOutcome decodes stdout as exactly one JSON document and returns its
// outcome field.
func jsonOutcome(t *testing.T, stdout string) string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc struct {
		Outcome string `json:"outcome"`
	}
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Errorf("stdout holds more than one JSON document: %q", stdout)
	}
	return doc.Outcome
}

func TestSelfUpgrade_JSONForEveryOutcome(t *testing.T) {
	t.Parallel()

	badArchive := func(t *testing.T) selfupdatetest.ServerOptions {
		t.Helper()
		rel := selfupdatetest.Release(t, "0.10.0")
		rel.Assets["checksums.txt"] = []byte(strings.Repeat("0", 64) + "  " +
			selfupdate.ArchiveName("0.10.0", runtime.GOOS, runtime.GOARCH) + "\n")
		return selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{rel}}
	}
	tests := []struct {
		name, rel, current string
		opts               func(*testing.T) selfupdatetest.ServerOptions
		args               []string
		code               int
		outcome            string
	}{
		{"upgraded", "bin/gskill", "0.9.0", nil, []string{"--yes"}, 0, "upgraded"},
		{"up to date", "bin/gskill", "0.10.0", nil, nil, 0, "up_to_date"},
		{"dry run", "bin/gskill", "0.9.0", nil, []string{"--dry-run"}, 0, "dry_run"},
		{"action required", "Caskroom/gskill/0.9.0/gskill", "0.9.0", nil, []string{"--yes"}, 8, "action_required"},
		{"checksum mismatch", "bin/gskill", "0.9.0", badArchive, []string{"--yes"}, 6, "failed"},
		{"no terminal and no --yes", "bin/gskill", "0.9.0", nil, nil, 1, "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := selfupdatetest.ServerOptions{}
			if tt.opts != nil {
				opts = tt.opts(t)
			}
			a, _, _ := selfUpgradeApp(t, tt.rel, tt.current, opts)
			args := append(append([]string{"--json"}, tt.args...), "self", "upgrade")
			stdout, stderr, code := runGskillWithApp(t, a, t.TempDir(), args...)
			if code != tt.code {
				t.Errorf("exit %d, want %d (stderr %q)", code, tt.code, stderr)
			}
			if got := jsonOutcome(t, stdout); got != tt.outcome {
				t.Errorf("outcome = %q, want %q", got, tt.outcome)
			}
		})
	}
}
