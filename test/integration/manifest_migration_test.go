package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pre023Project builds a project the way a spec 022 gskill would leave it: a
// lockfile and installed content, with no manifest.
func pre023Project(t *testing.T) (proj, repo string) {
	t.Helper()
	repo = gitRepo(t, validSkill("demo"), "v1.0.0")
	proj = newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	// Remove the manifest to reproduce a project installed before it existed.
	if err := os.Remove(manifestPath(proj)); err != nil {
		t.Fatal(err)
	}
	return proj, repo
}

// TestMigration_GeneratesManifest is FR-015: a project installed before the
// manifest existed gains one on the next mutating command. Without this every
// pre-023 project is stranded — overrides are unreachable without hand-writing
// a file whose schema the user has never seen.
func TestMigration_GeneratesManifest(t *testing.T) {
	t.Parallel()

	proj, _ := pre023Project(t)
	if _, err := os.Stat(manifestPath(proj)); !os.IsNotExist(err) {
		t.Fatal("fixture still has a manifest")
	}

	if _, stderr, code := runGskill(t, proj, "project", "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}

	got := readManifest(t, proj)
	if !strings.Contains(got, "[skills.demo]") {
		t.Errorf("manifest not generated for the installed skill:\n%s", got)
	}
	if !strings.Contains(got, "source =") {
		t.Errorf("generated entry has no source:\n%s", got)
	}
}

// TestMigration_RoundTripIsByteIdentical is FR-016 and the contract's own test:
// a generator that invents or drops a declaration would change what the next
// install resolves. If the lockfile moves, the generated manifest was lying
// about the intent the lock recorded.
func TestMigration_RoundTripIsByteIdentical(t *testing.T) {
	t.Parallel()

	proj, _ := pre023Project(t)
	before := readLock(t, proj)

	if _, stderr, code := runGskill(t, proj, "project", "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}

	if after := readLock(t, proj); after != before {
		t.Errorf("round-trip changed the lockfile\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// TestMigration_ForeignEntriesUntouched is FR-014: skills-lock.json is shared
// with other tools, so an entry gskill does not own must be absent from the
// manifest, unchanged in the lock, and never reported as an orphan.
func TestMigration_ForeignEntriesUntouched(t *testing.T) {
	t.Parallel()

	proj, _ := pre023Project(t)

	// Add an entry owned by another spec-012 tool.
	lockPath := filepath.Join(proj, "skills-lock.json")
	raw, err := os.ReadFile(lockPath) //nolint:gosec // test project
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	skills, _ := doc["skills"].(map[string]any)
	skills["other-tool-skill"] = map[string]any{
		"source": "github:someone/else", "sourceType": "git",
		"skillPath": "other-tool-skill", "computedHash": "sha256:cccc",
		"otherTool": map[string]any{"itsOwnField": "must survive"},
	}
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, append(updated, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "project", "sync"); code != 0 {
		t.Fatalf("sync: %s", stderr)
	}

	if got := readManifest(t, proj); strings.Contains(got, "other-tool-skill") {
		t.Errorf("foreign entry leaked into the manifest:\n%s", got)
	}
	if lock := readLock(t, proj); !strings.Contains(lock, "must survive") {
		t.Errorf("foreign entry damaged:\n%s", lock)
	}
}

// TestMigration_FrozenAndReadOnlyCreateNothing is FR-013 and FR-015: a frozen
// run must not create a declaration file, and a read-only command must not
// write at all.
func TestMigration_FrozenAndReadOnlyCreateNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"frozen install", []string{"install", "--frozen-lockfile"}},
		{"list", []string{"list"}},
		{"check", []string{"project", "check"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			proj, _ := pre023Project(t)

			if _, stderr, code := runGskill(t, proj, tc.args...); code != 0 && code != 7 {
				t.Fatalf("%v: exit %d: %s", tc.args, code, stderr)
			}
			if _, err := os.Stat(manifestPath(proj)); !os.IsNotExist(err) {
				t.Errorf("%v created a manifest", tc.args)
			}
		})
	}
}

// TestMigration_CheckTreatsAbsentManifestAsDerivable is FR-015 / US4 scenario 5:
// a pre-023 project is not broken, it is merely unmigrated. check must report
// its real health rather than failing over a file that the next mutating
// command will create.
func TestMigration_CheckTreatsAbsentManifestAsDerivable(t *testing.T) {
	t.Parallel()

	proj, _ := pre023Project(t)

	stdout, stderr, code := runGskill(t, proj, "project", "check")
	if code != 0 {
		t.Errorf("check on a healthy pre-023 project = exit %d, want 0\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(strings.ToLower(stderr), "skills.toml") &&
		strings.Contains(strings.ToLower(stderr), "error") {
		t.Errorf("check treated the absent manifest as an error:\n%s", stderr)
	}
}
