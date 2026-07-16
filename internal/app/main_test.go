package app_test

import (
	"os"
	"testing"

	"github.com/glapsfun/gskill/internal/home"
)

// TestMain isolates the entire app test process from the user's real gskill
// home: every test that resolves the global store (scope auto → global for
// fresh roots) operates under a throwaway GSKILL_HOME, never ~/.gskill.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gskill-test-home-*")
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
