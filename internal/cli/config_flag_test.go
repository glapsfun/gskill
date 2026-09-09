package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// levelDebug is the non-default log level these tests set from a file and
// then expect back out of the resolved configuration.
const levelDebug = "debug"

// writeConfigFile writes a TOML config file into dir and returns its path.
func writeConfigFile(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// configListPayload is the `gskill config list --json` document.
type configListPayload struct {
	Path   string            `json:"path"`
	Values map[string]string `json:"values"`
}

// readConfigList runs `config list --json` with the given global flags
// prepended and returns the decoded payload, failing on a non-zero exit.
func readConfigList(t *testing.T, a *app.App, globals ...string) configListPayload {
	t.Helper()

	args := append(append([]string{}, globals...), "--json", "config", "list")
	stdout, stderr, code := runCLI(t, a, args...)
	if code != 0 {
		t.Fatalf("gskill %v: exit %d (stderr: %q)", args, code, stderr)
	}
	var got configListPayload
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("config list --json is not valid JSON: %v\n%s", err, stdout)
	}
	return got
}

// projectWithConfig writes a manifest carrying a [config] table and returns
// the project root, so the project layer can be placed in a test.
func projectWithConfig(t *testing.T, body string) string {
	t.Helper()

	root := t.TempDir()
	writeConfigFile(t, root, "skills.toml", body)
	return root
}

// TestConfigFlag_ChangesEffectiveConfig is the regression test for issue #66:
// `--config` was declared, advertised in --help, and read by nothing, so a
// file passed to it changed nothing at all (spec 026 FR-001).
func TestConfigFlag_ChangesEffectiveConfig(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, t.TempDir(), "alt.toml", "log_level = \"debug\"\njobs = 7\n")

	got := readConfigList(t, newTestApp(), "--config", path)
	if got.Values["log_level"] != levelDebug {
		t.Errorf("log_level = %q, want %q from the named config file", got.Values["log_level"], levelDebug)
	}
	if got.Values["jobs"] != "7" {
		t.Errorf("jobs = %q, want %q from the named config file", got.Values["jobs"], "7")
	}
}

// TestConfigFlag_ConfigGetReadsNamedFile covers the singular read: `config
// get` must resolve the same configuration `config list` reports (FR-007).
func TestConfigFlag_ConfigGetReadsNamedFile(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, t.TempDir(), "alt.toml", "log_level = \"debug\"\n")

	stdout, stderr, code := runCLI(t, newTestApp(), "--config", path, "config", "get", "log_level")
	if code != 0 {
		t.Fatalf("config get: exit %d (stderr: %q)", code, stderr)
	}
	if got := trimLine(stdout); got != levelDebug {
		t.Errorf("config get log_level = %q, want %q", got, levelDebug)
	}
}

// TestConfigFlag_ReportedPathIsTheNamedFile pins FR-005 and FR-007 together:
// the named file replaces the discovered one, so the path `config list`
// advertises must be the one actually in effect. Reporting the discovered
// path here would recreate the "advertised but inert" defect being fixed.
func TestConfigFlag_ReportedPathIsTheNamedFile(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, t.TempDir(), "alt.toml", "jobs = 2\n")

	if got := readConfigList(t, newTestApp(), "--config", path); got.Path != path {
		t.Errorf("config list --json .path = %q, want the named file %q", got.Path, path)
	}
}

// TestConfigFlag_Precedence is the SC-002 gate, asserted end to end through
// the CLI rather than in the loader: config.Load's layer merge was never
// broken (internal/config's own precedence test proves it) — the wiring was.
//
// Only the merged layers appear here. Parsed flags are never folded into the
// resolved configuration; they are carried separately and read at the point
// of use, so they win by construction and have no merge-order pair to assert.
func TestConfigFlag_Precedence(t *testing.T) {
	// No t.Parallel: the env pair uses t.Setenv.
	userFile := writeConfigFile(t, t.TempDir(), "user.toml", "log_level = \"debug\"\njobs = 7\n")

	t.Run("user file beats defaults", func(t *testing.T) {
		t.Parallel()

		got := readConfigList(t, newTestApp(), "--config", userFile)
		if got.Values["log_level"] != levelDebug {
			t.Errorf("log_level = %q, want %q (user file over the built-in default)", got.Values["log_level"], levelDebug)
		}
	})

	t.Run("project beats user file", func(t *testing.T) {
		t.Parallel()

		root := projectWithConfig(t, "[config]\nlog_level = \"warn\"\n")

		got := readConfigList(t, newTestApp(), "-C", root, "--config", userFile)
		if got.Values["log_level"] != "warn" {
			t.Errorf("log_level = %q, want %q (skills.toml [config] over the user file)", got.Values["log_level"], "warn")
		}
		if got.Values["jobs"] != "7" {
			t.Errorf("jobs = %q, want %q (user file survives where the project is silent)", got.Values["jobs"], "7")
		}
	})

	t.Run("env beats project", func(t *testing.T) {
		t.Setenv("GSKILL_LOG_LEVEL", "error")
		root := projectWithConfig(t, "[config]\nlog_level = \"warn\"\n")

		got := readConfigList(t, newTestApp(), "-C", root, "--config", userFile)
		if got.Values["log_level"] != "error" {
			t.Errorf("log_level = %q, want %q (GSKILL_LOG_LEVEL over skills.toml [config])", got.Values["log_level"], "error")
		}
	})
}

// TestConfigFlag_ReachesEveryCommand proves FR-008/FR-009 without going
// through `config list`: the configuration the App hands to every command —
// and builds its logger from — carries the named file's values.
func TestConfigFlag_ReachesEveryCommand(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, t.TempDir(), "alt.toml", "log_level = \"debug\"\njobs = 7\n")

	a := newTestApp()
	if _, stderr, code := runCLI(t, a, "--config", path, "version"); code != 0 {
		t.Fatalf("version: exit %d (stderr: %q)", code, stderr)
	}
	if got := a.Config().LogLevel; got != levelDebug {
		t.Errorf("App.Config().LogLevel = %q, want %q — the file never reached the run", got, levelDebug)
	}
	if got := a.Config().Jobs; got != 7 {
		t.Errorf("App.Config().Jobs = %d, want 7 — the file never reached the run", got)
	}
}

// trimLine strips the trailing newline from a single-line command result.
func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// TestConfigFlag_MissingPathIsUsageError is the second half of issue #66: a
// path the user typed and misspelled exited 0 in silence, which is the worst
// available outcome for a CI job that believes it pinned its configuration
// (spec 026 FR-003).
func TestConfigFlag_MissingPathIsUsageError(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "nope.toml")

	stdout, stderr, code := runCLI(t, newTestApp(), "--config", missing, "config", "list")
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (usage error) for a nonexistent --config path", code)
	}
	if !strings.Contains(stderr, missing) {
		t.Errorf("stderr = %q, want it to name the offending path %q", stderr, missing)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty when the run fails", stdout)
	}
}

// TestConfigFlag_DirectoryIsUsageError covers the neighbouring mistake: a
// path that exists but is not a file.
func TestConfigFlag_DirectoryIsUsageError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, stderr, code := runCLI(t, newTestApp(), "--config", dir, "config", "list")
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (usage error) for a --config path that is a directory", code)
	}
	if !strings.Contains(stderr, dir) {
		t.Errorf("stderr = %q, want it to name the offending path %q", stderr, dir)
	}
}

// TestConfigFlag_ValidatedForCommandsThatIgnoreConfig proves the check is at
// parse time and therefore uniform: `version` reads no configuration, but a
// bad --config path is still a bad --config path.
func TestConfigFlag_ValidatedForCommandsThatIgnoreConfig(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "nope.toml")

	if _, stderr, code := runCLI(t, newTestApp(), "--config", missing, "version"); code != 2 {
		t.Errorf("gskill --config <missing> version: exit code = %d, want 2 (stderr: %q)", code, stderr)
	}
}

// TestConfigFlag_MalformedTOMLIsGenericError separates the two failure
// classes (contracts §3): a path that is not there is a bad *argument* and
// exits 2, while a file that will not parse is a bad *file* and exits 1 —
// which is what the startup loader already did before this feature.
func TestConfigFlag_MalformedTOMLIsGenericError(t *testing.T) {
	t.Parallel()

	bad := writeConfigFile(t, t.TempDir(), "broken.toml", "log_level = [\n")

	_, stderr, code := runCLI(t, newTestApp(), "--config", bad, "config", "list")
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (generic error) for a malformed config file", code)
	}
	if !strings.Contains(stderr, bad) {
		t.Errorf("stderr = %q, want it to name the offending path %q", stderr, bad)
	}
}

// TestConfigFlag_DoesNotMaskUsageErrors keeps FR-011 honest: adding config
// resolution to the run must not swallow or reorder the diagnostic a user
// gets for mistyping the command itself.
func TestConfigFlag_DoesNotMaskUsageErrors(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, t.TempDir(), "ok.toml", "jobs = 1\n")

	_, stderr, code := runCLI(t, newTestApp(), "--config", path, "definitely-not-a-command")
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr, "definitely-not-a-command") {
		t.Errorf("stderr = %q, want the unknown-command diagnostic, not a config one", stderr)
	}
}

// TestDiscoveredUserConfig_IsHonored is the third face of the same defect:
// `gskill config list` printed a user config path on its own first line, and
// a file placed at exactly that path changed nothing (spec 026 FR-004).
func TestDiscoveredUserConfig_IsHonored(t *testing.T) {
	// No t.Parallel: t.Setenv pins the discovered config directory.
	dir := t.TempDir()
	t.Setenv("GSKILL_CONFIG_DIR", dir)
	writeConfigFile(t, dir, "config.toml", "log_level = \"warn\"\njobs = 5\n")

	got := readConfigList(t, newTestApp())
	if got.Path != filepath.Join(dir, "config.toml") {
		t.Fatalf("config list .path = %q, want the discovered file in %q", got.Path, dir)
	}
	if got.Values["log_level"] != "warn" {
		t.Errorf("log_level = %q, want %q from the discovered user config file", got.Values["log_level"], "warn")
	}
	if got.Values["jobs"] != "5" {
		t.Errorf("jobs = %q, want %q from the discovered user config file", got.Values["jobs"], "5")
	}
}

// TestConfigFlag_ReplacesDiscoveredFile pins FR-005: --config replaces the
// discovered file rather than layering on top of it, so a key the named file
// leaves unset falls through to the defaults, not to the discovered file.
func TestConfigFlag_ReplacesDiscoveredFile(t *testing.T) {
	// No t.Parallel: t.Setenv pins the discovered config directory.
	dir := t.TempDir()
	t.Setenv("GSKILL_CONFIG_DIR", dir)
	writeConfigFile(t, dir, "config.toml", "log_level = \"warn\"\njobs = 5\n")
	named := writeConfigFile(t, t.TempDir(), "named.toml", "log_level = \"debug\"\n")

	got := readConfigList(t, newTestApp(), "--config", named)
	if got.Path != named {
		t.Errorf("config list .path = %q, want the named file %q", got.Path, named)
	}
	if got.Values["log_level"] != levelDebug {
		t.Errorf("log_level = %q, want %q from the named file", got.Values["log_level"], levelDebug)
	}
	if got.Values["jobs"] != "0" {
		t.Errorf("jobs = %q, want the default %q — the discovered file must not layer under the named one",
			got.Values["jobs"], "0")
	}
}

// TestDiscoveredUserConfig_AbsentIsSilent keeps FR-006 intact: not having a
// user config file is the normal case and must stay a silent exit 0. This is
// the boundary that makes the required-file rule safe to add.
func TestDiscoveredUserConfig_AbsentIsSilent(t *testing.T) {
	// No t.Parallel: t.Setenv pins the discovered config directory.
	t.Setenv("GSKILL_CONFIG_DIR", t.TempDir())

	stdout, stderr, code := runCLI(t, newTestApp(), "config", "list", "--no-interactive")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 with no user config file (stderr: %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty — an absent user config file is normal, not a diagnostic", stderr)
	}
	if !strings.Contains(stdout, "log_level = info") {
		t.Errorf("stdout = %q, want the built-in defaults", stdout)
	}
}

// TestDiscoveredUserConfig_MalformedIsGenericError: absence is fine, but a
// file that is there and will not parse must be reported — nothing else in
// the run would ever mention it.
func TestDiscoveredUserConfig_MalformedIsGenericError(t *testing.T) {
	// No t.Parallel: t.Setenv pins the discovered config directory.
	dir := t.TempDir()
	t.Setenv("GSKILL_CONFIG_DIR", dir)
	bad := writeConfigFile(t, dir, "config.toml", "log_level = [\n")

	_, stderr, code := runCLI(t, newTestApp(), "config", "list")
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (generic error) for a malformed discovered config file", code)
	}
	if !strings.Contains(stderr, bad) {
		t.Errorf("stderr = %q, want it to name the offending path %q", stderr, bad)
	}
}
