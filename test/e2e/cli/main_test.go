// Package cli_test drives the real gskill binary against local git fixtures.
// It has no build tag, so it runs in the default gate on every platform CI
// covers; the live-network suite one directory up stays behind the e2e tag.
package cli_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var gskillBin string

func TestMain(m *testing.M) {
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e/cli:", err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "gskill-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e/cli:", err)
		os.Exit(1)
	}
	gskillBin = filepath.Join(dir, "gskill")
	build := exec.CommandContext(context.Background(), "go", "build", "-trimpath", "-tags", "testseams", "-o", gskillBin, "./cmd/gskill") //nolint:gosec // builds the binary under test
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e/cli: build gskill: %v\n%s", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	if code == 0 {
		if missing := uncoveredCommands(); len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "e2e/cli: lifecycle commands never exercised: %v\n", missing)
			code = 1
		}
	}
	os.Exit(code)
}

// requiredCommands is the coverage contract (spec 024 FR-022): every one of
// these must be driven through the binary at least once per run.
var requiredCommands = []string{
	"add", "install", "install --frozen-lockfile", "update --list", "update",
	"upgrade", "verify", "check", "sync", "remove",
}

func uncoveredCommands() []string {
	var missing []string
	for _, c := range requiredCommands {
		if _, ok := invocations.Load(c); !ok {
			missing = append(missing, c)
		}
	}
	return missing
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
