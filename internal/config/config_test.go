package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/config"
)

func writeTOML(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoad_DefaultsWhenNoLayers(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(config.Sources{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, "info")
	}
	if cfg.Offline {
		t.Error("Offline = true, want default false")
	}
}

func TestLoad_PrecedenceFlagsOverEnvOverProjectOverUserOverDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	userFile := writeTOML(t, dir, "user.toml", "log_level = \"warn\"\noffline = true\n")
	projectFile := writeTOML(t, dir, "project.toml", "log_level = \"error\"\n")

	// User overrides defaults.
	cfg, err := config.Load(config.Sources{UserFile: userFile})
	if err != nil {
		t.Fatalf("Load user: %v", err)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("user layer: LogLevel = %q, want %q", cfg.LogLevel, "warn")
	}
	if !cfg.Offline {
		t.Error("user layer: Offline = false, want true")
	}

	// Project overrides user; user's offline still wins where project is silent.
	cfg, err = config.Load(config.Sources{UserFile: userFile, ProjectFile: projectFile})
	if err != nil {
		t.Fatalf("Load project: %v", err)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("project layer: LogLevel = %q, want %q", cfg.LogLevel, "error")
	}
	if !cfg.Offline {
		t.Error("project layer: Offline lost, want true carried from user")
	}

	// Env overrides project (with string coercion to the typed field).
	cfg, err = config.Load(config.Sources{
		UserFile:    userFile,
		ProjectFile: projectFile,
		Environ:     []string{"GSKILL_LOG_LEVEL=debug", "GSKILL_JOBS=4"},
	})
	if err != nil {
		t.Fatalf("Load env: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("env layer: LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.Jobs != 4 {
		t.Errorf("env layer: Jobs = %d, want 4 (string coerced)", cfg.Jobs)
	}

	// Flags override everything.
	cfg, err = config.Load(config.Sources{
		UserFile:    userFile,
		ProjectFile: projectFile,
		Environ:     []string{"GSKILL_LOG_LEVEL=debug"},
		Flags:       map[string]any{"log_level": "error", "offline": false},
	})
	if err != nil {
		t.Fatalf("Load flags: %v", err)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("flag layer: LogLevel = %q, want %q", cfg.LogLevel, "error")
	}
	if cfg.Offline {
		t.Error("flag layer: Offline = true, want false (flag override)")
	}
}

func TestLoad_MissingFilesAreSkipped(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(config.Sources{
		UserFile:    filepath.Join(t.TempDir(), "absent.toml"),
		ProjectFile: filepath.Join(t.TempDir(), "absent.toml"),
	})
	if err != nil {
		t.Fatalf("Load with missing files should succeed, got %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, "info")
	}
}

func TestDir_SuffixedWithApp(t *testing.T) {
	t.Parallel()

	dir, err := config.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if filepath.Base(dir) != "gskill" {
		t.Errorf("Dir base = %q, want %q (in %q)", filepath.Base(dir), "gskill", dir)
	}
}

func TestCacheDir_SuffixedWithApp(t *testing.T) {
	t.Parallel()

	dir, err := config.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if filepath.Base(dir) != "gskill" {
		t.Errorf("CacheDir base = %q, want %q (in %q)", filepath.Base(dir), "gskill", dir)
	}
}

func TestDir_EnvOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-config")
	t.Setenv("GSKILL_CONFIG_DIR", custom)

	dir, err := config.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if !strings.HasSuffix(dir, "custom-config") {
		t.Errorf("ConfigDir = %q, want override %q", dir, custom)
	}
}

func TestLoad_RepositoriesFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "repositories = [\"https://github.com/a/b\", \"https://github.com/c/d\"]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Sources{ProjectFile: path, Environ: []string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Repositories) != 2 || cfg.Repositories[0] != "https://github.com/a/b" {
		t.Errorf("Repositories = %v, want two repos", cfg.Repositories)
	}
}

func TestLoad_RepositoriesFromEnv(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(config.Sources{
		Environ: []string{"GSKILL_REPOSITORIES=https://github.com/a/b,https://github.com/c/d"},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Repositories) != 2 {
		t.Errorf("Repositories = %v, want 2 from env", cfg.Repositories)
	}
}

func TestLoad_RepositoriesDefaultEmpty(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(config.Sources{Environ: []string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Repositories) != 0 {
		t.Errorf("default Repositories = %v, want empty", cfg.Repositories)
	}
}
