package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stripManifestLine removes the first occurrence of a `key = ...` line from the
// project manifest, simulating a pre-008 manifest that omits that field.
func stripManifestLine(t *testing.T, proj, line string) {
	t.Helper()
	path := filepath.Join(proj, "gskill.toml")
	data := string(readFile(t, path))
	if !strings.Contains(data, line) {
		t.Fatalf("manifest does not contain %q:\n%s", line, data)
	}
	data = strings.Replace(data, line, "", 1)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUpdate_CaretFloatsToNewerTag is the finding-1 regression: a bare add records
// a caret range, so a newly published compatible tag is reported by `outdated`
// and installed by `update` (the exact pin would have frozen it).
func TestUpdate_CaretFloatsToNewerTag(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	demo := section(string(readFile(t, filepath.Join(proj, "gskill.toml"))), "[skills.demo]")
	if !strings.Contains(demo, "version = '^1.2.0'") {
		t.Fatalf("bare add did not record caret range:\n%s", demo)
	}

	// Publish a newer compatible tag.
	gitRun(t, repo, "tag", "v1.3.0")

	stdout, stderr, code := runGskill(t, proj, "outdated", "--json")
	if code != 0 {
		t.Fatalf("outdated: %s", stderr)
	}
	if !strings.Contains(stdout, `"available": true`) || !strings.Contains(stdout, `"latest": "1.3.0"`) {
		t.Errorf("outdated did not report 1.3.0 available:\n%s", stdout)
	}

	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	lock := string(readFile(t, filepath.Join(proj, "gskill.lock")))
	if !strings.Contains(lock, `"version": "1.3.0"`) {
		t.Errorf("update did not bump the lock to 1.3.0:\n%s", lock)
	}
	// The manifest constraint is unchanged (still the caret).
	demo = section(string(readFile(t, filepath.Join(proj, "gskill.toml"))), "[skills.demo]")
	if !strings.Contains(demo, "version = '^1.2.0'") {
		t.Errorf("update changed the manifest constraint:\n%s", demo)
	}
}

// TestSync_RecordsNewlyDetectedAgentNotStaleLock is the finding-2 regression: when
// a legacy manifest (no per-skill agents) is migrated by sync and a new agent has
// become detectable, the manifest records the full desired set actually installed
// — so a later prune does not delete the new target.
func TestSync_RecordsNewlyDetectedAgentNotStaleLock(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	// Strip the per-skill agents line (keep version so declarationUnchanged holds),
	// then make a second agent detectable.
	stripManifestLine(t, proj, "agents = ['claude']\n")
	if err := os.MkdirAll(filepath.Join(proj, ".codex"), 0o750); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}
	demo := section(string(readFile(t, filepath.Join(proj, "gskill.toml"))), "[skills.demo]")
	if !strings.Contains(demo, "agents = ['claude', 'codex']") {
		t.Errorf("sync recorded stale agent set instead of the desired one:\n%s", demo)
	}
	if _, err := os.Stat(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Fatalf("codex target not installed: %v", err)
	}

	// Second sync is a no-op, and prune must retain the now-declared codex target.
	stdout, stderr, code := runGskill(t, proj, "--json", "sync")
	if code != 0 {
		t.Fatalf("second sync: %s", stderr)
	}
	if !strings.Contains(stdout, `"up_to_date": true`) {
		t.Errorf("second sync not up to date:\n%s", stdout)
	}
	if _, stderr, code := runGskill(t, proj, "sync", "--prune"); code != 0 {
		t.Fatalf("sync --prune: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(proj, ".codex", "skills", "demo")); err != nil {
		t.Errorf("prune deleted the declared codex target: %v", err)
	}
}

// TestAddAgent_BackfillsLegacyVersionPin is the finding-3 regression: adding an
// agent to a pre-008 manifest with no version still backfills the version pin
// from the locked revision.
func TestAddAgent_BackfillsLegacyVersionPin(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0", "v1.2.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	// Simulate a pre-008 manifest that never recorded a version.
	stripManifestLine(t, proj, "version = '^1.2.0'\n")

	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "codex"); code != 0 {
		t.Fatalf("add codex: %s", stderr)
	}
	demo := section(string(readFile(t, filepath.Join(proj, "gskill.toml"))), "[skills.demo]")
	if !strings.Contains(demo, "version = '^1.2.0'") {
		t.Errorf("agent-add did not backfill the legacy version pin:\n%s", demo)
	}
	if !strings.Contains(demo, "agents = ['claude', 'codex']") {
		t.Errorf("agent-add did not union the agent set:\n%s", demo)
	}
}
