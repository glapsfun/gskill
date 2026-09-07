package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/testutil"
)

// setManifestVersion rewrites the version key of one declaration in place,
// the way a user edits skills.toml by hand.
func setManifestVersion(t *testing.T, proj, from, to string) {
	t.Helper()
	data := readFile(t, manifestPath(proj))
	if !bytes.Contains(data, []byte(`version = "`+from+`"`)) {
		t.Fatalf("manifest lacks version %q:\n%s", from, data)
	}
	out := bytes.Replace(data, []byte(`version = "`+from+`"`), []byte(`version = "`+to+`"`), 1)
	if err := os.WriteFile(manifestPath(proj), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func listItems(t *testing.T, proj string) map[string]map[string]any {
	t.Helper()
	stdout, stderr, code := runGskill(t, proj, "--json", "update", "--list", "--all")
	if code != 0 && code != 5 {
		t.Fatalf("update --list: code %d: %s", code, stderr)
	}
	obj := assertSingleJSON(t, stdout, "update --list")
	items := map[string]map[string]any{}
	skills, _ := obj["skills"].([]any)
	for _, raw := range skills {
		it, _ := raw.(map[string]any)
		name, _ := it["name"].(string)
		items[name] = it
	}
	return items
}

// TestUpdate_ManifestByteIdentical is spec 024 FR-003: a compatible update
// moves the lock and the installed content and never touches skills.toml.
func TestUpdate_ManifestByteIdentical(t *testing.T) {
	t.Parallel()

	proj := jsonUpdateProject(t)
	before := readFile(t, manifestPath(proj))
	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	if after := readFile(t, manifestPath(proj)); !bytes.Equal(before, after) {
		t.Errorf("update rewrote skills.toml:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if !strings.Contains(readLock(t, proj), `"version": "1.1.0"`) {
		t.Errorf("lock did not advance:\n%s", readLock(t, proj))
	}
}

// TestUpdate_ReadsIntentFromManifestNotLock is spec 024 FR-004: an edit to
// skills.toml is visible to update --list without an intervening install.
func TestUpdate_ReadsIntentFromManifestNotLock(t *testing.T) {
	t.Parallel()

	proj := jsonUpdateProject(t)
	setManifestVersion(t, proj, "^1.0.0", "1.0.0")

	demo := listItems(t, proj)["demo"]
	if demo["status"] != "pinned-version" {
		t.Fatalf("status = %v, want pinned-version from the edited manifest; item: %v", demo["status"], demo)
	}
	if demo["pinned"] != true || demo["shape"] != "exact-version" {
		t.Errorf("pinned/shape = %v/%v", demo["pinned"], demo["shape"])
	}
	if demo["next_action"] != "gskill install" {
		t.Errorf("next_action = %v, want gskill install (declaration changed)", demo["next_action"])
	}
	if reason, _ := demo["reason"].(string); !strings.Contains(reason, "declaration changed") {
		t.Errorf("reason = %v, want the declaration-changed note", demo["reason"])
	}
}

// TestUpdate_DeclarationChangedReason: a hand edit to a satisfiable version is
// reported as a pending declaration change, and update applies it.
func TestUpdate_DeclarationChangedReason(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")
	setManifestVersion(t, proj, "1.0.0", "1.1.0")

	demo := listItems(t, proj)["demo"]
	if reason, _ := demo["reason"].(string); !strings.Contains(reason, "declaration changed in skills.toml") {
		t.Fatalf("reason = %v, want declaration changed", demo["reason"])
	}
	if _, stderr, code := runGskill(t, proj, "update", "demo"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	lock := readLock(t, proj)
	if !strings.Contains(lock, `"version": "1.1.0"`) || !strings.Contains(lock, `"requestedVersion": "1.1.0"`) {
		t.Errorf("update did not apply the edited declaration:\n%s", lock)
	}
	if !strings.Contains(lock, `"declarationKind": "exact-version"`) {
		t.Errorf("lock lacks the declaration kind:\n%s", lock)
	}
}

// TestUpdate_BranchAdvancesToHead: branch tracking moves to the new head and
// the manifest is untouched.
func TestUpdate_BranchAdvancesToHead(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"))
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--ref", "main"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	before := readFile(t, manifestPath(proj))
	head := testutil.PublishCommit(t, repo, "demo", testutil.SkillBody("demo", "head2"))

	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	if !strings.Contains(readLock(t, proj), `"commit": "`+head+`"`) {
		t.Errorf("lock did not advance to the branch head %s:\n%s", head, readLock(t, proj))
	}
	if after := readFile(t, manifestPath(proj)); !bytes.Equal(before, after) {
		t.Errorf("update rewrote skills.toml")
	}
	if got := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")); !strings.Contains(string(got), "head2") {
		t.Errorf("installed content not at the new head:\n%s", got)
	}
}

// TestUpdate_ForeignEntriesIgnoredAndPreserved is spec 024 FR-020: a spec 012
// entry without a gskill block never appears in the report and survives an
// update byte for byte.
func TestUpdate_ForeignEntriesIgnoredAndPreserved(t *testing.T) {
	t.Parallel()

	proj := jsonUpdateProject(t)
	lockPath := filepath.Join(proj, "skills-lock.json")
	var doc map[string]any
	if err := json.Unmarshal(readFile(t, lockPath), &doc); err != nil {
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

	if _, present := listItems(t, proj)["other-tool-skill"]; present {
		t.Fatal("foreign entry appeared in update --list")
	}
	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	lock := readLock(t, proj)
	if !strings.Contains(lock, `"itsOwnField": "must survive"`) || !strings.Contains(lock, `"version": "1.1.0"`) {
		t.Errorf("foreign entry damaged or update missed:\n%s", lock)
	}
}
