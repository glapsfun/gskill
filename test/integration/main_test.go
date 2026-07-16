package integration_test

import (
	"os"
	"testing"

	"github.com/glapsfun/gskill/internal/home"
)

// TestMain isolates the integration test process from the user's real gskill
// home: any test that resolves the global store operates under a throwaway
// GSKILL_HOME, never ~/.gskill.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gskill-integ-home-*")
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
