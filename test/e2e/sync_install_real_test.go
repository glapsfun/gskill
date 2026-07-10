//go:build e2e

package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest writes a gskill.toml declaring the given skills (by known name)
// for the given agents, mirroring a hand-authored t1-style manifest.
func writeManifest(t *testing.T, root string, agents []string, skills ...string) {
	t.Helper()

	quoted := make([]string, len(agents))
	for i, a := range agents {
		quoted[i] = "'" + a + "'"
	}
	var b strings.Builder
	b.WriteString("schema_version = 1\n")
	for _, s := range skills {
		path, ok := knownSkills[s]
		if !ok {
			t.Fatalf("unknown skill %q", s)
		}
		b.WriteString("\n[skills." + s + "]\n")
		b.WriteString("source = '" + repoURL + "'\n")
		b.WriteString("path = '" + path + "'\n")
		b.WriteString("agents = [" + strings.Join(quoted, ", ") + "]\n")
	}
	if err := os.WriteFile(filepath.Join(root, "gskill.toml"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestE2E_SyncFromManifest covers scenario 4: reconcile from a declared manifest
// materializes the full chain, and a second sync is a byte-identical no-op
// (SC-004).
func TestE2E_SyncFromManifest(t *testing.T) {
	requireE2E(t)

	proj := newProject(t)
	writeManifest(t, proj, []string{"claude", "codex"}, "argocd", "fluxcd", "helm")

	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("first sync exit %d: %s", code, stderr)
	}
	for skill := range knownSkills {
		assertChain(t, proj, skill, "claude", "codex")
	}

	tomlBefore := readManifest(t, proj)
	lockBefore, err := os.ReadFile(filepath.Join(proj, "skills-lock.json")) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runGskill(t, proj, "--json", "sync")
	if code != 0 {
		t.Fatalf("second sync exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, `"up_to_date": true`) {
		t.Errorf("second sync not up to date:\n%s", stdout)
	}
	if readManifest(t, proj) != tomlBefore {
		t.Error("manifest rewritten on idempotent re-sync")
	}
	lockAfter, err := os.ReadFile(filepath.Join(proj, "skills-lock.json")) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lockAfter, lockBefore) {
		t.Error("lockfile rewritten on idempotent re-sync")
	}
}

// TestE2E_InstallFromManifest covers scenario 5: install from a declared
// manifest records the resolved revision and agents in the lock, and a re-run
// reports no change.
func TestE2E_InstallFromManifest(t *testing.T) {
	requireE2E(t)

	proj := newProject(t)
	writeManifest(t, proj, []string{"claude"}, "helm")

	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install exit %d: %s", code, stderr)
	}
	assertChain(t, proj, "helm", "claude")

	lock, err := os.ReadFile(filepath.Join(proj, "skills-lock.json")) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"commit":`, `"content_hash":`, `"claude"`} {
		if !strings.Contains(string(lock), want) {
			t.Errorf("lockfile missing %q", want)
		}
	}

	stdout, stderr, code := runGskill(t, proj, "install", "--json")
	if code != 0 {
		t.Fatalf("re-install exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, `"changed": true`) {
		t.Errorf("re-install reported changes:\n%s", stdout)
	}
}
