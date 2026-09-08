package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

func exactPinProject(t *testing.T) (proj, repo string) {
	t.Helper()
	repo = gitRepo(t, validSkill("demo"), "v1.0.0")
	proj = newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")
	return proj, repo
}

// TestUpdateList_PinnedVersionRowAndHint is spec 024 US2: an exact version
// is reported as pinned, with the declaration that pins it and the way to
// move it.
func TestUpdateList_PinnedVersionRowAndHint(t *testing.T) {
	t.Parallel()

	proj, _ := exactPinProject(t)
	stdout, stderr, code := runGskill(t, proj, "update", "--list", "--all")
	if code != 0 {
		t.Fatalf("update --list --all exit %d: %s", code, stderr)
	}
	for _, want := range []string{"pinned version", `pinned by skills.toml (version = "1.0.0")`, "newest 1.1.0", "gskill upgrade demo"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	items := listItems(t, proj)
	for _, it := range items {
		if it["status"] == "unknown" {
			t.Errorf("retired status appeared: %v", it)
		}
	}
}

// TestUpdateList_AllPinnedSummaryCountsByStatus: an all-pinned project never
// claims to be "up to date"; the summary counts what was hidden.
func TestUpdateList_AllPinnedSummaryCountsByStatus(t *testing.T) {
	t.Parallel()

	proj, _ := exactPinProject(t)
	stdout, stderr, code := runGskill(t, proj, "update", "--list")
	if code != 0 {
		t.Fatalf("update --list exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, "All skills are up to date") {
		t.Errorf("all-pinned project claims up to date:\n%s", stdout)
	}
	if !strings.Contains(stdout, "1 pinned") || !strings.Contains(stdout, "--all") {
		t.Errorf("summary does not count the hidden pinned skill:\n%s", stdout)
	}
}

// TestUpdate_ApplyPinnedGuidanceLineAndExitZero: applying an update to a
// pinned skill is an honest no-op with guidance, never a failure.
func TestUpdate_ApplyPinnedGuidanceLineAndExitZero(t *testing.T) {
	t.Parallel()

	proj, _ := exactPinProject(t)
	lockBefore := readLock(t, proj)
	stdout, stderr, code := runGskill(t, proj, "--no-interactive", "update")
	if code != 0 {
		t.Fatalf("update exit %d: %s", code, stderr)
	}
	for _, want := range []string{"pinned version", "pinned by skills.toml", "gskill upgrade demo", "1 pinned"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	for _, never := range []string{"error", "failed"} {
		if strings.Contains(strings.ToLower(stdout), never) {
			t.Errorf("stdout reads as a failure (%q):\n%s", never, stdout)
		}
	}
	if readLock(t, proj) != lockBefore {
		t.Error("update rewrote the lock of a pinned skill")
	}
}

// TestUpdateList_LookupFailedStatusExit5: an unreachable source is a distinct
// status, and the report exits 5 after rendering.
func TestUpdateList_LookupFailedStatusExit5(t *testing.T) {
	t.Parallel()

	proj := jsonUpdateProject(t)
	m := readFile(t, manifestPath(proj))
	src := regexp.MustCompile(`source\s*=\s*"([^"]+)"`).FindSubmatch(m)
	if src == nil {
		t.Fatalf("no source in manifest:\n%s", m)
	}
	if err := os.RemoveAll(string(src[1])); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runGskill(t, proj, "update", "--list", "--all")
	if code != 5 {
		t.Errorf("exit = %d, want 5", code)
	}
	if !strings.Contains(stdout, "lookup failed") || !strings.Contains(stdout, "could not be checked") {
		t.Errorf("stdout hides the failed lookup:\n%s", stdout)
	}
	if demo := listItems(t, proj)["demo"]; demo["status"] != "lookup-failed" {
		t.Errorf("status = %v, want lookup-failed", demo["status"])
	}
}

// TestUpdateList_OfflineLookupFailedExit0: offline, a floating skill is
// "lookup failed (offline)" and the report still exits 0.
func TestUpdateList_OfflineLookupFailedExit0(t *testing.T) {
	t.Parallel()

	proj := jsonUpdateProject(t)
	stdout, _, code := runGskill(t, proj, "--offline", "--json", "update", "--list", "--all")
	if code != 0 {
		t.Fatalf("offline list exit %d:\n%s", code, stdout)
	}
	obj := assertSingleJSON(t, stdout, "offline list")
	skills, _ := obj["skills"].([]any)
	demo, _ := skills[0].(map[string]any)
	reason, _ := demo["reason"].(string)
	if demo["status"] != "lookup-failed" || !strings.Contains(reason, "offline") {
		t.Errorf("offline item = %v", demo)
	}
}

// TestUpdate_NoProjectFilesExitsZero: nothing declared is not an error.
func TestUpdate_NoProjectFilesExitsZero(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	stdout, stderr, code := runGskill(t, proj, "--no-interactive", "update")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "No skills declared.") {
		t.Errorf("stdout = %q", stdout)
	}
}

// narrowAgents drops codex from one declaration, the way a user edits
// skills.toml by hand to stop targeting an agent.
func narrowAgents(t *testing.T, proj string) {
	t.Helper()
	data := readFile(t, manifestPath(proj))
	out := bytes.Replace(data, []byte(`agents = ["claude", "codex"]`), []byte(`agents = ["claude"]`), 1)
	if bytes.Equal(data, out) {
		t.Fatalf("could not narrow the agent list in:\n%s", data)
	}
	if err := os.WriteFile(manifestPath(proj), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUpdate_NarrowedAgentsRemovesTarget: install already removes the target
// of an agent dropped from the manifest. update must do the same. Before the
// fix it replaced the lock record with the narrowed agent set while leaving
// the codex symlink on disk — an orphan no later command tracks, still
// pointing at the active path update had just rewritten, so the dropped agent
// silently kept reading new content.
func TestUpdate_NarrowedAgentsRemovesTarget(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0", "--agent", "claude,codex"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	codexTarget := filepath.Join(proj, ".codex", "skills", "demo")
	if _, err := os.Stat(codexTarget); err != nil {
		t.Fatalf("codex target missing after add: %v", err)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")
	narrowAgents(t, proj)

	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	if _, err := os.Stat(codexTarget); err == nil {
		t.Error("update left the dropped agent's target behind as an untracked orphan")
	}
	if _, stderr, code := runGskill(t, proj, "verify"); code != 0 {
		t.Errorf("verify after update: %s", stderr)
	}
}

// TestUpdate_RefEditMovesToDeclaredBranch: the manifest is authoritative for
// intent, so changing ref from main to release must make update follow
// release. Discovery used to probe the locked branch, so an unchanged main
// reported "up to date" and update never moved to release at all.
func TestUpdate_RefEditMovesToDeclaredBranch(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--ref", "main"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	testutil.GitRun(t, repo, "checkout", "--quiet", "-b", "release")
	testutil.PublishCommit(t, repo, "demo",
		"---\nname: demo\ndescription: updated\n---\n# demo on-release\n")
	testutil.GitRun(t, repo, "checkout", "--quiet", "main")

	setManifestKey(t, proj, "demo", "ref", "release")
	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	content := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))
	if !strings.Contains(string(content), "on-release") {
		t.Errorf("update did not follow the declared branch:\n%s", content)
	}
}
