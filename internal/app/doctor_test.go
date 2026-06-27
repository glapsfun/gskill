package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// projectWithLock writes a minimal lockfile carrying the given JSON.
func projectWithLock(t *testing.T, lockJSON string) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gskill.lock"), []byte(lockJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func newDoctorApp() *app.App {
	return app.New(app.Options{Agents: agent.NewDefaultRegistry()})
}

func TestDoctor_ReportsGitAndDetectedAgents(t *testing.T) {
	t.Parallel()

	root := projectWithLock(t, `{"lockfile_version":1,"skills":{}}`)
	report, err := newDoctorApp().Doctor(context.Background(), root)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	found := false
	for _, id := range report.DetectedAgents {
		if id == "claude-code" {
			found = true
		}
	}
	if !found {
		t.Errorf("claude-code not detected: %v", report.DetectedAgents)
	}
}

func TestDoctor_WarnsOnUnmetRequirements(t *testing.T) {
	t.Parallel()

	lock := `{"lockfile_version":1,"skills":{"demo":{` +
		`"source":{"type":"git","original":"x/y"},"requested":{},` +
		`"resolved":{"ref_kind":"semver","content_hash":"sha256:x","mutable_ref":false},` +
		`"metadata":{"name":"demo","description":"d"},` +
		`"requires":{"commands":["definitely-not-a-real-binary-zzz"],"environment":["GSKILL_DEFINITELY_UNSET_ZZZ"],"skills":[],"mcp":["some-server"]},` +
		`"installation":{"scope":"project","mode":"symlink","agents":["claude-code"],"targets":{}},` +
		`"provenance":{"trust":"checksum-ok"}}}}`
	root := projectWithLock(t, lock)

	report, err := newDoctorApp().Doctor(context.Background(), root)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if len(report.Warnings) == 0 {
		t.Error("expected warnings for unmet command/environment requirements")
	}
}

// Regression for FR-032: requirements are recorded and warned only; gskill never
// attempts to resolve or auto-install tooling, runtimes, or MCP servers.
func TestDoctor_RequirementsAreSurfacedNeverInstalled(t *testing.T) {
	t.Parallel()

	lock := `{"lockfile_version":1,"skills":{"demo":{` +
		`"source":{"type":"git","original":"x/y"},"requested":{},` +
		`"resolved":{"ref_kind":"semver","content_hash":"sha256:x","mutable_ref":false},` +
		`"metadata":{"name":"demo","description":"d"},` +
		`"requires":{"commands":["kubectl"],"environment":[],"skills":[],"mcp":["github-mcp"]},` +
		`"installation":{"scope":"project","mode":"symlink","agents":["claude-code"],"targets":{}},` +
		`"provenance":{"trust":"checksum-ok"}}}}`
	root := projectWithLock(t, lock)

	report, err := newDoctorApp().Doctor(context.Background(), root)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	// MCP requirements are surfaced but explicitly not checked (never installed).
	var sawMCP bool
	for _, rc := range report.Requirements {
		if rc.Kind == "mcp" {
			sawMCP = true
			if rc.Checked {
				t.Errorf("mcp requirement %q marked as checked; gskill must not resolve MCP servers", rc.Name)
			}
		}
	}
	if !sawMCP {
		t.Error("mcp requirement was not surfaced at all")
	}
}
