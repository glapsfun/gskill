package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/home"
)

// TestMain isolates the CLI test process from the user's real gskill home.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gskill-cli-home-*")
	if err != nil {
		panic(err)
	}
	if os.Getenv(home.EnvHome) == "" {
		_ = os.Setenv(home.EnvHome, dir)
	}
	// Same isolation for the config directory: without it the discovered user
	// config file (spec 026) would be the developer's own, making config tests
	// depend on the machine they run on. Individual tests still override this
	// with t.Setenv when they need a file at the discovered path.
	if os.Getenv(config.EnvConfigDir) == "" {
		_ = os.Setenv(config.EnvConfigDir, filepath.Join(dir, "config"))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
