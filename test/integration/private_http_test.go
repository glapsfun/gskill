package integration_test

import (
	"io/fs"
	"net/http"
	"net/http/cgi" //nolint:gosec // serves git http-backend to a loopback test client; the httpoxy note targets Go before 1.6.3
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/testutil"
)

// plantedSecret is the password the private stand-in requires. No file
// gskill writes and no output it prints may contain it (spec 035 SC-004).
const plantedSecret = "SECRET-035"

// privateServer serves bare repositories under root with git http-backend
// behind Basic auth (user u, password plantedSecret). It counts requests
// that carried the right credentials.
type privateServer struct {
	*httptest.Server
	authorized atomic.Int64
}

func newPrivateServer(t *testing.T, root string) *privateServer {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "--exec-path").Output()
	if err != nil {
		t.Skipf("git --exec-path: %v", err)
	}
	backend := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("git http-backend is not installed (%v); the private HTTPS stand-in needs it", err)
	}
	cgiHandler := &cgi.Handler{
		Path:       backend,
		Env:        []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
		InheritEnv: []string{"PATH"},
	}
	s := &privateServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "u" || pass != plantedSecret {
			w.Header().Set("WWW-Authenticate", `Basic realm="private"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		s.authorized.Add(1)
		cgiHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// hermeticGitConfig keeps the machine's Git config out of the test: no
// system or global config, and so no credential helper such as osxkeychain
// that could store or supply the password. helper, when set, is the only
// credential helper. Callers must not be
// parallel.
func hermeticGitConfig(t *testing.T, helper string, rewrites map[string]string) {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")

	keys := []string{"credential.helper"}
	values := []string{""}
	if helper != "" {
		keys, values = append(keys, "credential.helper"), append(values, helper)
	}
	for base, prefix := range rewrites {
		keys, values = append(keys, "url."+base+".insteadOf"), append(values, prefix)
	}
	t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(len(keys)))
	for i := range keys {
		t.Setenv("GIT_CONFIG_KEY_"+strconv.Itoa(i), keys[i])
		t.Setenv("GIT_CONFIG_VALUE_"+strconv.Itoa(i), values[i])
	}
}

// secretHelper answers every credential request with the planted password.
const secretHelper = "!f() { echo username=u; echo password=" + plantedSecret + "; }; f"

// bareRepo publishes fixture as root/<repoPath>.git for http-backend.
func bareRepo(t *testing.T, root, fixture, repoPath string) {
	t.Helper()
	dst := filepath.Join(root, repoPath+".git")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatal(err)
	}
	testutil.GitRun(t, root, "clone", "--quiet", "--bare", fixture, dst)
}

// assertNoSecret fails when any planted secret appears in the outputs or in
// any file under the given directories.
func assertNoSecret(t *testing.T, outputs []string, dirs ...string) {
	t.Helper()
	for _, out := range outputs {
		if strings.Contains(out, plantedSecret) {
			t.Errorf("output contains the planted secret:\n%s", out)
		}
	}
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, rerr := os.ReadFile(path) //nolint:gosec // test-owned tree
			if rerr == nil && strings.Contains(string(data), plantedSecret) {
				t.Errorf("%s contains the planted secret", path)
			}
			return nil
		})
	}
}

var privateHosts = []struct{ name, source, repoPath string }{
	{"github", "https://github.com/acme/skills.git", "acme/skills"},
	{"gitlab", "https://gitlab.com/acme/platform/skills.git", "acme/platform/skills"},
	{"bitbucket", "https://bitbucket.org/acme/skills.git", "acme/skills"},
}

// TestPrivateHTTP runs the lifecycle over HTTPS against a stand-in that
// requires credentials, supplied only by a Git credential helper (spec 035
// SC-001, SC-004, FR-017).
func TestPrivateHTTP(t *testing.T) { //nolint:paralleltest // subtests set GIT_CONFIG_*
	for _, h := range privateHosts { //nolint:paralleltest // subtests set GIT_CONFIG_*
		t.Run(h.name, func(t *testing.T) {
			root := t.TempDir()
			bareRepo(t, root, providerFixture(t), h.repoPath)
			srv := newPrivateServer(t, root)
			hermeticGitConfig(t, secretHelper, map[string]string{srv.URL + "/" + h.repoPath + ".git": h.source})

			a := noAPIApp(t)
			proj := newProject(t)
			var outputs []string
			run := func(dir string, args ...string) {
				t.Helper()
				stdout, stderr, code := runGskillWithApp(t, a, dir, append([]string{"--no-interactive"}, args...)...)
				outputs = append(outputs, stdout, stderr)
				if code != 0 {
					t.Fatalf("gskill %v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
				}
			}
			run(proj, "add", h.source, "--skill", "demo", "--version", "^1.0.0", "--agent", "claude")
			if e := lockEntryOf(t, proj, "demo"); e.Gskill.SourceURL != h.source || e.Gskill.Version != "1.1.0" {
				t.Fatalf("add locked %q at %q, want %q at 1.1.0", e.Gskill.SourceURL, e.Gskill.Version, h.source)
			}
			if srv.authorized.Load() == 0 {
				t.Fatal("the stand-in served no authorized request; the test did not exercise credentials")
			}

			teammate := freshCopy(t, proj)
			fresh := noAPIApp(t)
			stdout, stderr, code := runGskillWithApp(t, fresh, teammate, "--no-interactive", "install", "--frozen-lockfile")
			outputs = append(outputs, stdout, stderr)
			if code != 0 {
				t.Fatalf("frozen install: exit %d\n%s", code, stderr)
			}
			run(proj, "update")
			run(proj, "--json", "upgrade", "demo", "--to", "2.0.0")
			run(proj, "--json", "project", "verify")

			assertNoSecret(t, outputs, proj, teammate, a.GskillHome(), fresh.GskillHome())
		})
	}
}

// TestPrivateHTTP_NoCredentials: without a credential helper and without a
// terminal, every command that needs the private repository fails promptly
// with the authentication code (spec 035 FR-018, SC-003).
func TestPrivateHTTP_NoCredentials(t *testing.T) { //nolint:paralleltest // sets GIT_CONFIG_*
	h := privateHosts[1]
	root := t.TempDir()
	bareRepo(t, root, providerFixture(t), h.repoPath)
	srv := newPrivateServer(t, root)
	rewrite := map[string]string{srv.URL + "/" + h.repoPath + ".git": h.source}

	// An authorized run writes the lock a teammate without access receives.
	hermeticGitConfig(t, secretHelper, rewrite)
	proj := newProject(t)
	runOK(t, noAPIApp(t), proj, "add", h.source, "--skill", "demo", "--version", "^1.0.0", "--agent", "claude")

	hermeticGitConfig(t, "", rewrite)
	// update acts on several skills, so it reports the failure per skill and
	// exits with the partial-failure code, as for any other per-skill error.
	cases := []struct {
		name string
		dir  string
		args []string
		code int
	}{
		{"add", newProject(t), []string{"add", h.source, "--skill", "demo", "--agent", "claude"}, 11},
		{"frozen install", freshCopy(t, proj), []string{"install", "--frozen-lockfile"}, 11},
		{"update", freshCopy(t, proj), []string{"update"}, 10},
	}
	for _, tc := range cases {
		start := time.Now()
		stdout, stderr, code := runGskillWithApp(t, noAPIApp(t), tc.dir, append([]string{"--no-interactive"}, tc.args...)...)
		if elapsed := time.Since(start); elapsed > 30*time.Second {
			t.Errorf("%s took %s without credentials, want under 30s", tc.name, elapsed)
		}
		if code != tc.code {
			t.Errorf("%s: exit %d, want %d\nstdout: %s\nstderr: %s", tc.name, code, tc.code, stdout, stderr)
		}
		if !strings.Contains(stderr+stdout, "credentials for gitlab.com are missing, expired, or not allowed to read gitlab.com/acme/platform/skills") {
			t.Errorf("%s: output does not name the host and repository:\n%s%s", tc.name, stdout, stderr)
		}
		assertNoSecret(t, []string{stdout, stderr})
	}
}
