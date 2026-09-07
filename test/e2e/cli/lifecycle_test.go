package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/testutil"
)

const agentClaude = "claude"

// TestLifecycle_AddCreatesManifestLockAndInstall covers the first step of the
// critical lifecycle: an empty project gains a manifest, a lock, and installed
// content from one `add`, and `verify` agrees with all three (spec 024 US5).
func TestLifecycle_AddCreatesManifestLockAndInstall(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)

	res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude)
	if res.Code != 0 {
		t.Fatalf("add: code=%d stderr=%s", res.Code, res.Stderr)
	}
	assertNotContains(t, "add stderr", res.Stderr, "error:")

	manifest := manifestBytes(t, dir)
	assertContains(t, "skills.toml", string(manifest), "[skills.demo]", `version = "^1.0.0"`)
	entry := lockEntry(t, dir, "demo")
	if entry.Version != "1.0.0" || entry.RequestedVersion != "^1.0.0" {
		t.Fatalf("lock entry = %+v", entry)
	}
	assertContains(t, "installed content", installed(t, dir, agentClaude, "demo"), "# demo v1.0.0")

	if res := run(t, dir, "verify"); res.Code != 0 {
		t.Fatalf("verify: code=%d stderr=%s", res.Code, res.Stderr)
	}

	lockBefore := lockBytes(t, dir)
	if res := run(t, dir, "install"); res.Code != 0 {
		t.Fatalf("install: code=%d stderr=%s", res.Code, res.Stderr)
	}
	assertUnchanged(t, "skills-lock.json after idempotent install", lockBefore, lockBytes(t, dir))
	assertUnchanged(t, "skills.toml after idempotent install", manifest, manifestBytes(t, dir))
}

// TestLifecycle_CompatibleUpdate is the critical scenario of spec 024 US1:
// a newer compatible release moves the lock and the installed content, the
// manifest is byte-identical, and verify agrees.
func TestLifecycle_CompatibleUpdate(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)
	if res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude); res.Code != 0 {
		t.Fatalf("add: %s", res.Stderr)
	}
	manifestBefore := manifestBytes(t, dir)
	lockBefore := lockBytes(t, dir)

	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v1.1.0"), "v1.1.0")
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v2.0.0"), "v2.0.0")

	list := run(t, dir, "update", "--list")
	if list.Code != 0 {
		t.Fatalf("update --list: %s", list.Stderr)
	}
	assertContains(t, "update --list", list.Stdout, "demo", "1.0.0", "1.1.0")
	assertNotContains(t, "update --list", list.Stdout, "2.0.0")
	assertUnchanged(t, "skills.toml after --list", manifestBefore, manifestBytes(t, dir))
	assertUnchanged(t, "skills-lock.json after --list", lockBefore, lockBytes(t, dir))

	if res := run(t, dir, "update"); res.Code != 0 {
		t.Fatalf("update: %s", res.Stderr)
	}
	assertUnchanged(t, "skills.toml after update", manifestBefore, manifestBytes(t, dir))
	entry := lockEntry(t, dir, "demo")
	if entry.Version != "1.1.0" || entry.RequestedVersion != "^1.0.0" {
		t.Fatalf("lock entry after update = %+v, want 1.1.0 within ^1.0.0", entry)
	}
	assertContains(t, "installed content", installed(t, dir, agentClaude, "demo"), "# demo v1.1.0")
	if res := run(t, dir, "verify"); res.Code != 0 {
		t.Fatalf("verify: %s", res.Stderr)
	}
}

// TestLifecycle_ExactPinsReportPinned is spec 024 US2 through the binary: an
// exact version and an exact commit are reported as pinned with guidance,
// nothing changes on any layer, and the exit code is 0.
func TestLifecycle_ExactPinsReportPinned(t *testing.T) {
	t.Parallel()
	dir := newProject(t)
	verRepo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	commitRepo := skillRepo(t, "other", "v1.0.0", "v1.0.0")
	sha := testutil.GitOutput(t, commitRepo, "rev-parse", "HEAD")
	if res := run(t, dir, "add", verRepo, "--skill", "demo", "--agent", agentClaude, "--version", "1.0.0"); res.Code != 0 {
		t.Fatalf("add demo: %s", res.Stderr)
	}
	if res := run(t, dir, "add", commitRepo, "--skill", "other", "--agent", agentClaude, "--commit", sha); res.Code != 0 {
		t.Fatalf("add other: %s", res.Stderr)
	}
	manifestBefore, lockBefore := manifestBytes(t, dir), lockBytes(t, dir)
	contentBefore := installed(t, dir, agentClaude, "demo") + installed(t, dir, agentClaude, "other")

	testutil.PublishVersion(t, verRepo, "demo", testutil.SkillBody("demo", "v1.1.0"), "v1.1.0")
	testutil.PublishVersion(t, commitRepo, "other", testutil.SkillBody("other", "v1.1.0"), "v1.1.0")

	list := run(t, dir, "update", "--list", "--all")
	if list.Code != 0 {
		t.Fatalf("update --list --all: code %d %s", list.Code, list.Stderr)
	}
	assertContains(t, "update --list --all", list.Stdout, "pinned version", "pinned commit", "gskill upgrade demo", "gskill upgrade other")

	apply := run(t, dir, "--no-interactive", "update")
	if apply.Code != 0 {
		t.Fatalf("update: code %d %s", apply.Code, apply.Stderr)
	}
	assertContains(t, "update", apply.Stdout, "pinned version", "pinned commit", "2 pinned")
	assertNotContains(t, "update", strings.ToLower(apply.Stdout), "error", "failed")

	assertUnchanged(t, "skills.toml", manifestBefore, manifestBytes(t, dir))
	assertUnchanged(t, "skills-lock.json", lockBefore, lockBytes(t, dir))
	if got := installed(t, dir, agentClaude, "demo") + installed(t, dir, agentClaude, "other"); got != contentBefore {
		t.Fatalf("installed content changed under pinned update")
	}
}

// TestLifecycle_MajorUpgrade is the second critical scenario of spec 024:
// update stays on 1.x because of the constraint, upgrade moves manifest,
// lock, and content to 2.x, and verify agrees.
func TestLifecycle_MajorUpgrade(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)
	if res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude); res.Code != 0 {
		t.Fatalf("add: %s", res.Stderr)
	}
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v1.1.0"), "v1.1.0")
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v2.0.0"), "v2.0.0")

	if res := run(t, dir, "update"); res.Code != 0 {
		t.Fatalf("update: %s", res.Stderr)
	}
	if e := lockEntry(t, dir, "demo"); e.Version != "1.1.0" {
		t.Fatalf("update left lock at %s, want 1.1.0 (constraint ^1.0.0)", e.Version)
	}
	list := run(t, dir, "update", "--list", "--all")
	assertContains(t, "update --list --all", list.Stdout, "newer 2.0.0 outside ^1.0.0", "gskill upgrade demo")
	manifestBefore := manifestBytes(t, dir)

	up := run(t, dir, "upgrade", "demo", "--latest")
	if up.Code != 0 {
		t.Fatalf("upgrade: code %d %s\n%s", up.Code, up.Stderr, up.Stdout)
	}
	assertContains(t, "upgrade", up.Stdout, "upgraded", "verified")
	after := manifestBytes(t, dir)
	if strings.Count(string(after), "\n") != strings.Count(string(manifestBefore), "\n") || !strings.Contains(string(after), `version = "^2.0.0"`) {
		t.Fatalf("manifest after upgrade:\n%s", after)
	}
	if e := lockEntry(t, dir, "demo"); e.Version != "2.0.0" || e.RequestedVersion != "^2.0.0" {
		t.Fatalf("lock entry after upgrade = %+v", e)
	}
	assertContains(t, "installed content", installed(t, dir, agentClaude, "demo"), "# demo v2.0.0")
	if res := run(t, dir, "verify"); res.Code != 0 {
		t.Fatalf("verify: %s", res.Stderr)
	}
}

// TestLifecycle_UpgradeRollbackOnFailure: a refused target writes nothing,
// and an interrupt after the manifest write restores every layer.
func TestLifecycle_UpgradeRollbackOnFailure(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)
	if res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude); res.Code != 0 {
		t.Fatalf("add: %s", res.Stderr)
	}
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v2.0.0"), "v2.0.0")
	manifestBefore, lockBefore := manifestBytes(t, dir), lockBytes(t, dir)
	contentBefore := installed(t, dir, agentClaude, "demo")

	refused := run(t, dir, "upgrade", "demo", "--to", "9.9.9")
	if refused.Code != 2 {
		t.Fatalf("refused upgrade exit = %d, want 2: %s", refused.Code, refused.Stderr)
	}
	assertContains(t, "refusal", refused.Stderr, "9.9.9")
	assertUnchanged(t, "skills.toml after refusal", manifestBefore, manifestBytes(t, dir))
	assertUnchanged(t, "skills-lock.json after refusal", lockBefore, lockBytes(t, dir))

	interrupted := runUntilPaused(t, dir, "upgrade", "demo", "--latest")
	if interrupted.Code != 130 {
		t.Fatalf("interrupted upgrade exit = %d, want 130\n%s", interrupted.Code, interrupted.Stderr)
	}
	assertUnchanged(t, "skills.toml after interrupt", manifestBefore, manifestBytes(t, dir))
	assertUnchanged(t, "skills-lock.json after interrupt", lockBefore, lockBytes(t, dir))
	if got := installed(t, dir, agentClaude, "demo"); got != contentBefore {
		t.Fatalf("installed content changed after interrupt:\n%s", got)
	}
	if res := run(t, dir, "check"); res.Code != 0 {
		t.Fatalf("check after rollback: %s", res.Stderr)
	}
}

func editManifestVersion(t *testing.T, dir, from, to string) {
	t.Helper()
	m := manifestBytes(t, dir)
	out := strings.Replace(string(m), `version = "`+from+`"`, `version = "`+to+`"`, 1)
	if out == string(m) {
		t.Fatalf("manifest has no version %q:\n%s", from, m)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills.toml"), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLifecycle_ManualEditThenInstall is spec 024 US4: a hand edit to the
// declared version is realized by one plain install.
func TestLifecycle_ManualEditThenInstall(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)
	if res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude, "--version", "1.0.0"); res.Code != 0 {
		t.Fatalf("add: %s", res.Stderr)
	}
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v1.3.0"), "v1.3.0")
	editManifestVersion(t, dir, "1.0.0", "1.3.0")

	if res := run(t, dir, "install"); res.Code != 0 {
		t.Fatalf("install: %s", res.Stderr)
	}
	if e := lockEntry(t, dir, "demo"); e.Version != "1.3.0" || e.RequestedVersion != "1.3.0" {
		t.Fatalf("lock entry = %+v, want 1.3.0", e)
	}
	assertContains(t, "installed content", installed(t, dir, agentClaude, "demo"), "# demo v1.3.0")
	if res := run(t, dir, "verify"); res.Code != 0 {
		t.Fatalf("verify: %s", res.Stderr)
	}
}

// TestLifecycle_FrozenRejectsEditedManifest: under --frozen-lockfile a
// declaration that disagrees with the lock exits 4 and writes nothing.
func TestLifecycle_FrozenRejectsEditedManifest(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)
	if res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude); res.Code != 0 {
		t.Fatalf("add: %s", res.Stderr)
	}
	editManifestVersion(t, dir, "^1.0.0", "^2.0.0")
	manifestBefore, lockBefore := manifestBytes(t, dir), lockBytes(t, dir)

	res := run(t, dir, "install", "--frozen-lockfile")
	if res.Code != 4 {
		t.Fatalf("frozen install exit = %d, want 4: %s", res.Code, res.Stderr)
	}
	assertContains(t, "frozen stderr", res.Stderr, "demo", "version")
	assertUnchanged(t, "skills.toml", manifestBefore, manifestBytes(t, dir))
	assertUnchanged(t, "skills-lock.json", lockBefore, lockBytes(t, dir))
}

// TestLifecycle_PreManifestProjectMigrates is spec 024 FR-019: a project
// with a lock but no manifest classifies identically before and after the
// manifest is generated, including an entry whose lock carries no requested
// intent at all.
func TestLifecycle_PreManifestProjectMigrates(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)
	if res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude, "--ref", "v1.0.0"); res.Code != 0 {
		t.Fatalf("add: %s", res.Stderr)
	}
	// Strip the recorded intent so only the resolved tag remains, the shape of
	// a lock written before intent was projected into it.
	lockPath := filepath.Join(dir, "skills-lock.json")
	var doc map[string]any
	if err := json.Unmarshal(lockBytes(t, dir), &doc); err != nil {
		t.Fatal(err)
	}
	skills, _ := doc["skills"].(map[string]any)
	entry, _ := skills["demo"].(map[string]any)
	g, _ := entry["gskill"].(map[string]any)
	st, _ := g["state"].(map[string]any)
	delete(st, "requestedRef")
	delete(st, "requestedVersion")
	delete(st, "declarationKind")
	raw, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(lockPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "skills.toml")); err != nil {
		t.Fatal(err)
	}

	before := run(t, dir, "--json", "update", "--list", "--all")
	if before.Code != 0 {
		t.Fatalf("list before: %s", before.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills.toml")); err == nil {
		t.Fatal("a read-only command created skills.toml")
	}
	if res := run(t, dir, "--no-interactive", "update"); res.Code != 0 {
		t.Fatalf("update: %s", res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills.toml")); err != nil {
		t.Fatal("update did not generate skills.toml")
	}
	after := run(t, dir, "--json", "update", "--list", "--all")
	if after.Code != 0 {
		t.Fatalf("list after: %s", after.Stderr)
	}
	var b, a map[string]any
	if err := json.Unmarshal([]byte(before.Stdout), &b); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(after.Stdout), &a); err != nil {
		t.Fatal(err)
	}
	bs, _ := json.Marshal(b["skills"])
	as, _ := json.Marshal(a["skills"])
	if string(bs) != string(as) {
		t.Fatalf("classification changed across manifest generation:\n--- before ---\n%s\n--- after ---\n%s", bs, as)
	}
	assertContains(t, "classification", string(as), `"status":"pinned-tag"`)
}

// TestLifecycle_CheckSyncRemove closes the lifecycle: a missing agent link is
// drift that sync repairs; tampered committed content is drift that check and
// verify report and only repair restores (the repository owns it); remove
// retires the manifest and lock entries together.
func TestLifecycle_CheckSyncRemove(t *testing.T) {
	t.Parallel()
	repo := skillRepo(t, "demo", "v1.0.0", "v1.0.0")
	dir := newProject(t)
	if res := run(t, dir, "add", repo, "--skill", "demo", "--agent", agentClaude); res.Code != 0 {
		t.Fatalf("add: %s", res.Stderr)
	}
	if res := run(t, dir, "check", "--fail-on-drift"); res.Code != 0 {
		t.Fatalf("check on a clean project: code %d %s", res.Code, res.Stderr)
	}
	assertSyncRepairsMissingLink(t, dir)
	assertTamperFailsClosedUntilRepair(t, dir)

	if res := run(t, dir, "--yes", "remove", "demo"); res.Code != 0 {
		t.Fatalf("remove: code %d %s", res.Code, res.Stderr)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".claude", "skills", "demo")); err == nil {
		t.Fatal("agent link survived remove")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "skills.toml")); err == nil && strings.Contains(string(b), "[skills.demo]") { //nolint:gosec // test path
		t.Fatalf("manifest still declares demo:\n%s", b)
	}
	if strings.Contains(string(lockBytes(t, dir)), `"demo"`) {
		t.Fatal("lock still records demo")
	}
}

func assertSyncRepairsMissingLink(t *testing.T, dir string) {
	t.Helper()
	link := filepath.Join(dir, ".claude", "skills", "demo")
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if res := run(t, dir, "check", "--fail-on-drift"); res.Code != 7 {
		t.Fatalf("check with a missing link: code %d, want 7\n%s%s", res.Code, res.Stdout, res.Stderr)
	}
	if res := run(t, dir, "sync"); res.Code != 0 {
		t.Fatalf("sync: code %d %s", res.Code, res.Stderr)
	}
	assertContains(t, "content after sync", installed(t, dir, agentClaude, "demo"), "# demo v1.0.0")
	if res := run(t, dir, "verify"); res.Code != 0 {
		t.Fatalf("verify after sync: %s", res.Stderr)
	}
}

func assertTamperFailsClosedUntilRepair(t *testing.T, dir string) {
	t.Helper()
	committed := filepath.Join(dir, ".agents", "skills", "demo", "SKILL.md")
	if err := os.WriteFile(committed, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := run(t, dir, "check", "--fail-on-drift"); res.Code != 7 {
		t.Fatalf("check after tamper: code %d, want 7", res.Code)
	}
	if res := run(t, dir, "verify"); res.Code != 6 {
		t.Fatalf("verify after tamper: code %d, want 6", res.Code)
	}
	if res := run(t, dir, "sync"); res.Code == 0 {
		t.Fatal("sync must fail closed on tampered committed content")
	}
	if res := run(t, dir, "repair"); res.Code != 0 {
		t.Fatalf("repair: code %d %s", res.Code, res.Stderr)
	}
	if res := run(t, dir, "verify"); res.Code != 0 {
		t.Fatalf("verify after repair: %s", res.Stderr)
	}
}

// localSkillDir creates a plain directory holding one skill, for the local
// source kind.
func localSkillDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(testutil.SkillBody(name, "local")), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRegression_UpdateNeverWritesManifest is the SC-007 guard: for every
// declaration kind, update leaves skills.toml byte-identical — with and
// without something newer upstream.
func TestRegression_UpdateNeverWritesManifest(t *testing.T) {
	t.Parallel()
	dir := newProject(t)
	rangeRepo := skillRepo(t, "ranged", "v1.0.0", "v1.0.0")
	exactRepo := skillRepo(t, "exact", "v1.0.0", "v1.0.0")
	tagRepo := skillRepo(t, "tagged", "v1.0.0", "v1.0.0")
	branchRepo := skillRepo(t, "branched", "v1.0.0")
	commitRepo := skillRepo(t, "committed", "v1.0.0")
	sha := testutil.GitOutput(t, commitRepo, "rev-parse", "HEAD")
	local := localSkillDir(t, "local")

	adds := [][]string{
		{"add", rangeRepo, "--skill", "ranged", "--agent", agentClaude},
		{"add", exactRepo, "--skill", "exact", "--agent", agentClaude, "--version", "1.0.0"},
		{"add", tagRepo, "--skill", "tagged", "--agent", agentClaude, "--ref", "v1.0.0"},
		{"add", branchRepo, "--skill", "branched", "--agent", agentClaude, "--ref", "main"},
		{"add", commitRepo, "--skill", "committed", "--agent", agentClaude, "--commit", sha},
		{"add", local, "--agent", agentClaude},
	}
	for _, args := range adds {
		if res := run(t, dir, args...); res.Code != 0 {
			t.Fatalf("%v: %s", args, res.Stderr)
		}
	}
	before := manifestBytes(t, dir)
	for _, kind := range []string{"ranged", "exact", "tagged"} {
		testutil.PublishVersion(t, map[string]string{"ranged": rangeRepo, "exact": exactRepo, "tagged": tagRepo}[kind], kind, testutil.SkillBody(kind, "v1.1.0"), "v1.1.0")
	}
	testutil.PublishCommit(t, branchRepo, "branched", testutil.SkillBody("branched", "head2"))
	testutil.PublishCommit(t, commitRepo, "committed", testutil.SkillBody("committed", "head2"))

	if res := run(t, dir, "--no-interactive", "update"); res.Code != 0 {
		t.Fatalf("update: code %d\n%s%s", res.Code, res.Stdout, res.Stderr)
	}
	assertUnchanged(t, "skills.toml after update across every kind", before, manifestBytes(t, dir))
	if e := lockEntry(t, dir, "ranged"); e.Version != "1.1.0" {
		t.Errorf("ranged did not move: %+v", e)
	}
	if e := lockEntry(t, dir, "exact"); e.Version != "1.0.0" {
		t.Errorf("exact moved: %+v", e)
	}
	assertContains(t, "update output", res2(t, dir).Stdout, "pinned version", "pinned tag", "pinned commit", "local source")
}

func res2(t *testing.T, dir string) result {
	t.Helper()
	return run(t, dir, "--no-interactive", "update")
}
