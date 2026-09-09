package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glapsfun/gskill/internal/testutil"
)

type result struct {
	Stdout, Stderr string
	Code           int
}

// invocations records every subcommand the suite exercised, for the
// command-coverage test.
var invocations sync.Map // string -> bool

// groupCommands are the command groups whose leaf must be recorded as a
// two-token invocation ("project verify"), so the coverage contract in
// requiredCommands can name a leaf instead of the bare group. Without this,
// only a flag-shaped second token was ever joined, and the maintenance
// commands would silently collapse to "project" once spec 025 moved them off
// their flat aliases.
var groupCommands = map[string]bool{"project": true, "cache": true, "config": true}

func record(args []string) {
	if len(args) == 0 {
		return
	}
	var parts []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") && parts == nil {
			continue
		}
		parts = append(parts, a)
		if len(parts) == 2 {
			break
		}
	}
	invocations.Store(parts[0], true)
	if len(parts) == 2 && (strings.HasPrefix(parts[1], "--") || groupCommands[parts[0]]) {
		invocations.Store(parts[0]+" "+parts[1], true)
	}
	for _, a := range args {
		if a == "--frozen-lockfile" || a == "--list" {
			invocations.Store(parts[0]+" "+a, true)
		}
	}
}

// run executes the built binary in dir with a scrubbed environment and returns
// the separated streams and exit code. Every call is logged so a CI failure is
// diagnosable from the test output alone.
func run(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runEnv(t, dir, nil, args...)
}

func runEnv(t *testing.T, dir string, extra []string, args ...string) result {
	t.Helper()
	record(args)
	cmd := exec.CommandContext(context.Background(), gskillBin, args...)
	cmd.Dir = dir
	cmd.Env = append(scrubbedEnv(t, dir), extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run gskill %v: %v", args, err)
	}
	t.Logf("gskill %s -> %d\n%s", strings.Join(args, " "), code, strings.TrimSpace(stderr.String()))
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

// scrubbedEnv builds the subprocess environment from scratch: a private HOME
// and GSKILL_HOME per project directory, PATH for git, and nothing inherited
// that could reach the developer's real state or the network.
func scrubbedEnv(t *testing.T, dir string) []string {
	t.Helper()
	home := filepath.Join(filepath.Dir(dir), "home-"+filepath.Base(dir))
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatal(err)
	}
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GSKILL_HOME=" + filepath.Join(home, ".gskill"),
		// Explicit, though HOME already isolates it: the binary resolves a
		// user config file (spec 026), and that isolation should not rest on
		// the platform's HOME-derived default staying HOME-derived.
		"GSKILL_CONFIG_DIR=" + filepath.Join(home, ".config", "gskill"),
		"NO_COLOR=1",
		"TERM=dumb",
		"GOFLAGS=-mod=mod",
	}
}

// newProject returns an empty project directory carrying a Claude Code marker
// so agent detection succeeds.
func newProject(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	return root
}

func manifestBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "skills.toml")) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read skills.toml: %v", err)
	}
	return b
}

func lockBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "skills-lock.json")) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read skills-lock.json: %v", err)
	}
	return b
}

type lockView struct {
	Version          string
	Commit           string
	ContentHash      string
	RequestedVersion string
	RequestedRef     string
	RequestedCommit  string
	Raw              map[string]any
}

// lockEntry parses the skill's entry out of skills-lock.json.
func lockEntry(t *testing.T, dir, name string) lockView {
	t.Helper()
	var doc struct {
		Skills map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(lockBytes(t, dir), &doc); err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	e, ok := doc.Skills[name]
	if !ok {
		t.Fatalf("lock has no entry %q; keys: %v", name, keys(doc.Skills))
	}
	v := lockView{Raw: e}
	if g, ok := e["gskill"].(map[string]any); ok {
		v.Version, _ = g["version"].(string)
		v.Commit, _ = g["commit"].(string)
		v.ContentHash, _ = g["contentHash"].(string)
		if st, ok := g["state"].(map[string]any); ok {
			v.RequestedVersion, _ = st["requestedVersion"].(string)
			v.RequestedRef, _ = st["requestedRef"].(string)
			v.RequestedCommit, _ = st["requestedCommit"].(string)
		}
	}
	return v
}

func keys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// installed returns the content the agent sees for a skill.
func installed(t *testing.T, dir, agent, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "."+agent, "skills", name, "SKILL.md")) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read installed %s/%s: %v", agent, name, err)
	}
	return string(b)
}

func assertUnchanged(t *testing.T, what string, before, after []byte) {
	t.Helper()
	if !bytes.Equal(before, after) {
		t.Fatalf("%s changed:\n--- before ---\n%s\n--- after ---\n%s", what, before, after)
	}
}

func assertContains(t *testing.T, what, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			t.Fatalf("%s lacks %q:\n%s", what, n, haystack)
		}
	}
}

func assertNotContains(t *testing.T, what, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			t.Fatalf("%s must not contain %q:\n%s", what, n, haystack)
		}
	}
}

func skillRepo(t *testing.T, name, marker string, tags ...string) string {
	t.Helper()
	return testutil.InitSkillRepo(t, name, testutil.SkillBody(name, marker), tags...)
}

func TestHarness_BuildsAndRunsVersion(t *testing.T) {
	t.Parallel()
	res := run(t, newProject(t), "version")
	if res.Code != 0 || strings.TrimSpace(res.Stdout) == "" {
		t.Fatalf("version: code=%d stdout=%q stderr=%q", res.Code, res.Stdout, res.Stderr)
	}
}

// runUntilPaused starts the binary with GSKILL_TEST_PAUSE set, waits for the
// "paused" marker on stderr, sends SIGINT, and returns the final result. The
// testseams build honours the pause; the marker makes the interrupt
// deterministic instead of racing the install.
func runUntilPaused(t *testing.T, dir string, args ...string) result {
	t.Helper()
	record(args)
	cmd := exec.CommandContext(context.Background(), gskillBin, args...)
	cmd.Dir = dir
	cmd.Env = append(scrubbedEnv(t, dir), "GSKILL_TEST_PAUSE=before-activate")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	buf := make([]byte, 4096)
	for {
		n, rerr := pr.Read(buf)
		stderr.Write(buf[:n])
		if strings.Contains(stderr.String(), "paused") {
			_ = cmd.Process.Signal(os.Interrupt)
			break
		}
		if rerr != nil {
			break
		}
	}
	rest, _ := io.ReadAll(pr)
	stderr.Write(rest)
	werr := cmd.Wait()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(werr, &exitErr) {
		code = exitErr.ExitCode()
	}
	t.Logf("gskill %s (interrupted) -> %d\n%s", strings.Join(args, " "), code, strings.TrimSpace(stderr.String()))
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}
