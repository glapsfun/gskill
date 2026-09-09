package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/home"
)

// TestMain isolates the integration test process from the user's real gskill
// home and configuration directory: any test that resolves the global store
// operates under a throwaway GSKILL_HOME, never ~/.gskill, and none of them
// read the developer's own config.toml.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gskill-integ-home-*")
	if err != nil {
		panic(err)
	}
	if os.Getenv(home.EnvHome) == "" {
		_ = os.Setenv(home.EnvHome, dir)
	}
	// Same isolation for the configuration directory: these tests drive
	// cli.Run in-process, which resolves the user config file (spec 026), so
	// without this a developer's own config.toml would change test outcomes.
	if os.Getenv(config.EnvConfigDir) == "" {
		_ = os.Setenv(config.EnvConfigDir, filepath.Join(dir, "config"))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
