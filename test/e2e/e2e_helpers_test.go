//go:build e2e

// Package e2e_test holds opt-in, real-source end-to-end tests that drive the
// gskill CLI against the live github.com/glapsfun/cnative-skills repository.
//
// They are excluded from the default build (the `e2e` build tag) and from the
// verify gate, and additionally skip unless GSKILL_E2E=1 and git is available,
// so the standard `go test ./...` stays hermetic and offline. Run them with:
//
//	GSKILL_E2E=1 go test -tags=e2e ./test/e2e/...
package e2e_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/cli"
)

// repoURL is the live multi-skill source the e2e scenarios install from.
const repoURL = "https://github.com/glapsfun/cnative-skills.git"

// knownSkills are stable skills the named scenarios target by name, with their
// in-repo paths (plugins/<x>/skills/<x>).
var knownSkills = map[string]string{
	"argocd": "plugins/argocd/skills/argocd",
	"fluxcd": "plugins/fluxcd/skills/fluxcd",
	"helm":   "plugins/helm/skills/helm",
}

// agentMarker maps an agent id to its project marker directory.
var agentMarker = map[string]string{
	"claude":     ".claude",
	"codex":      ".codex",
	"cursor":     ".cursor",
	"gemini-cli": ".gemini",
}

// requireE2E skips unless the opt-in flag is set and git is available (FR-013).
func requireE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("GSKILL_E2E") != "1" {
		t.Skip("set GSKILL_E2E=1 to run real-source e2e tests")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// newApp builds an App with a discard logger and the default agent registry.
func newApp() *app.App {
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// runGskill runs the CLI against root in-process and returns stdout, stderr, exit.
func runGskill(t *testing.T, root string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	full := append([]string{"-C", root}, args...)
	var out, errb bytes.Buffer
	code = cli.Run(context.Background(), full, &out, &errb, newApp())
	return out.String(), errb.String(), code
}

// newProject creates an initialized project with a Claude Code marker.
func newProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskill(t, root, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	return root
}

// readManifest returns the project's gskill.toml contents.
func readManifest(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "gskill.toml")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// section returns the lines of the named TOML table up to the next table header.
func section(toml, header string) string {
	idx := strings.Index(toml, header)
	if idx < 0 {
		return ""
	}
	rest := toml[idx+len(header):]
	if end := strings.Index(rest, "\n["); end >= 0 {
		return rest[:end]
	}
	return rest
}

// assertChain verifies the three-layer chain (active entry + each agent target)
// and that the manifest records the agent set and a version pin (FR-012).
func assertChain(t *testing.T, root, skill string, agents ...string) {
	t.Helper()

	if _, err := os.Lstat(filepath.Join(root, ".agents", "skills", skill)); err != nil {
		t.Errorf("active entry missing for %s: %v", skill, err)
	}
	for _, ag := range agents {
		marker, ok := agentMarker[ag]
		if !ok {
			t.Fatalf("unknown agent marker for %q", ag)
		}
		if _, err := os.Stat(filepath.Join(root, marker, "skills", skill)); err != nil {
			t.Errorf("agent target missing %s/skills/%s: %v", marker, skill, err)
		}
	}

	entry := section(readManifest(t, root), "[skills."+skill+"]")
	if !strings.Contains(entry, "agents = [") {
		t.Errorf("manifest %s entry missing agents:\n%s", skill, entry)
	}
	if !strings.Contains(entry, "version =") &&
		!strings.Contains(entry, "ref =") &&
		!strings.Contains(entry, "commit =") {
		t.Errorf("manifest %s entry missing a version pin:\n%s", skill, entry)
	}
}

// countStoreEntries returns the number of content-addressed store directories.
func countStoreEntries(t *testing.T, root string) int {
	t.Helper()
	algoDir := filepath.Join(root, ".gskill", "store", "sha256")
	entries, err := os.ReadDir(algoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	return len(entries)
}
