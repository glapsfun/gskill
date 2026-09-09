package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// writeFile writes body to dir/name and returns the path.
func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestApplyRuntimeConfig_MergesUserFileProjectAndEnv is the app-level half of
// spec 026: the layers that only become knowable after the command line is
// parsed — the named user file and the project's [config] table — are merged
// in one pass, in the documented order (FR-002).
func TestApplyRuntimeConfig_MergesUserFileProjectAndEnv(t *testing.T) {
	// No t.Parallel: t.Setenv pins the environment layer for this test.
	dir := t.TempDir()
	userFile := writeFile(t, dir, "user.toml", "log_level = \"debug\"\njobs = 7\noffline = true\n")
	root := t.TempDir()
	writeFile(t, root, "skills.toml", "[config]\nlog_level = \"warn\"\n")

	a := app.New(app.Options{})
	if err := a.ApplyRuntimeConfig(root, userFile); err != nil {
		t.Fatalf("ApplyRuntimeConfig: %v", err)
	}

	cfg := a.Config()
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q (project [config] beats the user file)", cfg.LogLevel, "warn")
	}
	if cfg.Jobs != 7 {
		t.Errorf("Jobs = %d, want 7 (user file, project silent)", cfg.Jobs)
	}
	if !cfg.Offline {
		t.Error("Offline = false, want true (user file, project silent)")
	}

	// Environment beats both.
	t.Setenv("GSKILL_LOG_LEVEL", "error")
	if err := a.ApplyRuntimeConfig(root, userFile); err != nil {
		t.Fatalf("ApplyRuntimeConfig with env: %v", err)
	}
	if got := a.Config().LogLevel; got != "error" {
		t.Errorf("LogLevel = %q, want %q (env beats project)", got, "error")
	}
}

// TestApplyRuntimeConfig_ReportsMalformedFile locks the error that
// ApplyProjectConfig used to swallow: a config file that will not parse must
// fail the run and name itself (spec 026 FR-003).
func TestApplyRuntimeConfig_ReportsMalformedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.toml", "log_level = [\n")

	err := app.New(app.Options{}).ApplyRuntimeConfig(t.TempDir(), bad)
	if err == nil {
		t.Fatal("ApplyRuntimeConfig returned nil for a malformed config file, want an error")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("error %q does not name the offending path %q", err, bad)
	}
}

// TestApplyRuntimeConfig_ReportsMissingNamedFile is FR-003 at the app
// boundary: a userFile the caller named explicitly is required, even though a
// discovered one is not.
func TestApplyRuntimeConfig_ReportsMissingNamedFile(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent.toml")

	err := app.New(app.Options{}).ApplyRuntimeConfig(t.TempDir(), missing)
	if err == nil {
		t.Fatal("ApplyRuntimeConfig returned nil for a missing named config file, want an error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the offending path %q", err, missing)
	}
}

// TestApplyRuntimeConfig_ConfigFileReportsPathInEffect pins the value
// `gskill config list` prints: the named file when one is given, the
// discovered path otherwise (FR-004, FR-005, FR-007).
func TestApplyRuntimeConfig_ConfigFileReportsPathInEffect(t *testing.T) {
	// No t.Parallel: t.Setenv pins the discovered config directory.
	configDir := t.TempDir()
	t.Setenv("GSKILL_CONFIG_DIR", configDir)

	a := app.New(app.Options{})
	if err := a.ApplyRuntimeConfig(t.TempDir(), ""); err != nil {
		t.Fatalf("ApplyRuntimeConfig: %v", err)
	}
	if want := filepath.Join(configDir, "config.toml"); a.ConfigFile() != want {
		t.Errorf("ConfigFile() = %q, want the discovered path %q", a.ConfigFile(), want)
	}

	named := writeFile(t, t.TempDir(), "named.toml", "jobs = 3\n")
	if err := a.ApplyRuntimeConfig(t.TempDir(), named); err != nil {
		t.Fatalf("ApplyRuntimeConfig named: %v", err)
	}
	if a.ConfigFile() != named {
		t.Errorf("ConfigFile() = %q, want the named path %q", a.ConfigFile(), named)
	}
}
