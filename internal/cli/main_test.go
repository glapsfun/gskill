package cli_test

import (
	"os"
	"testing"

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
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
