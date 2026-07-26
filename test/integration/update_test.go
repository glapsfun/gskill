package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/testutil"
)

// publishNewVersion commits a content change to repo's skill dir and tags it.
func publishNewVersion(t *testing.T, repo, skillName, tag string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, skillName, "SKILL.md"),
		[]byte("---\nname: "+skillName+"\ndescription: updated\n---\n# "+skillName+" "+tag+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "--quiet", "-m", tag)
	gitRun(t, repo, "tag", tag)
}

func TestUpdateList_ReportsCandidatesOnStdout(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")

	lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))
	stdout, _, code := runGskill(t, proj, "update", "--list")
	if code != 0 {
		t.Fatalf("update --list exit = %d, want 0", code)
	}

	for _, col := range []string{"NAME", "CURRENT", "AVAILABLE", "POLICY"} {
		if !strings.Contains(stdout, col) {
			t.Errorf("stdout missing %q column:\n%s", col, stdout)
		}
	}
	row := regexp.MustCompile(`demo\s+1\.0\.0\s+1\.1\.0\s+\^1\.0\.0`)
	if !row.MatchString(stdout) {
		t.Errorf("stdout missing candidate row (SC-003):\n%s", stdout)
	}
	if !strings.Contains(stdout, "1 update available") {
		t.Errorf("stdout missing count summary:\n%s", stdout)
	}

	// Read-only: the lockfile must be byte-identical (FR-006).
	lockAfter := readFile(t, filepath.Join(proj, "skills-lock.json"))
	if string(lockBefore) != string(lockAfter) {
		t.Error("update --list modified skills-lock.json")
	}

	// Deterministic: a repeated run renders identical output (FR-018).
	again, _, code := runGskill(t, proj, "update", "--list")
	if code != 0 || again != stdout {
		t.Errorf("repeated update --list differs (code %d):\n%q\n%q", code, stdout, again)
	}
}

func TestUpdateList_AllReportsNonActionableStatuses(t *testing.T) {
	t.Parallel()

	pinRepo := gitRepo(t, validSkill("deploy"), "v1.0.0", "v2.0.0")
	freshRepo := gitRepo(t, validSkill("fresh"), "v3.0.0")
	local := localSkillDir(t, "local-tools")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", pinRepo, "--ref", "v1.0.0"); code != 0 {
		t.Fatalf("add pin: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", freshRepo, "--version", "^3.0.0"); code != 0 {
		t.Fatalf("add fresh: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", local); code != 0 {
		t.Fatalf("add local: %s", stderr)
	}

	// The exact tag pin never appears as an actionable update (FR-004).
	stdout, _, code := runGskill(t, proj, "update", "--list")
	if code != 0 {
		t.Fatalf("update --list exit = %d, want 0", code)
	}
	if strings.Contains(stdout, "deploy") {
		t.Errorf("pinned tag listed as actionable:\n%s", stdout)
	}

	// --all explains every skill with a status (FR-007).
	stdout, _, code = runGskill(t, proj, "update", "--list", "--all")
	if code != 0 {
		t.Fatalf("update --list --all exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "STATUS") {
		t.Errorf("--all output missing STATUS column:\n%s", stdout)
	}
	for skill, status := range map[string]string{
		"deploy":      "pinned tag (newer: 2.0.0)",
		"fresh":       "up to date",
		"local-tools": "local source",
	} {
		if !strings.Contains(stdout, skill) || !strings.Contains(stdout, status) {
			t.Errorf("--all output missing %s / %q:\n%s", skill, status, stdout)
		}
	}
}

func TestUpdateNamed_RendersFromToResult(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	other := gitRepo(t, validSkill("other"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", other, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add other: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")
	publishNewVersion(t, other, "other", "v1.1.0")

	stdout, stderr, code := runGskill(t, proj, "update", "demo")
	if code != 0 {
		t.Fatalf("update demo exit = %d: %s", code, stderr)
	}
	for _, col := range []string{"NAME", "FROM", "TO", "RESULT"} {
		if !strings.Contains(stdout, col) {
			t.Errorf("stdout missing %q column:\n%s", col, stdout)
		}
	}
	if !regexp.MustCompile(`demo\s+1\.0\.0\s+1\.1\.0\s+updated`).MatchString(stdout) {
		t.Errorf("stdout missing from/to transition (FR-015):\n%s", stdout)
	}

	// Only the named skill moved (FR-011).
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.1.0"`) {
		t.Errorf("demo did not advance:\n%s", lock)
	}
	if !strings.Contains(lock, `"version": "1.0.0"`) {
		t.Errorf("other advanced despite not being named:\n%s", lock)
	}
}

func TestUpdateNamed_UpToDateIsHonestNoOp(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))
	stdout, _, code := runGskill(t, proj, "update", "demo")
	if code != 0 {
		t.Errorf("no-op named update exit = %d, want 0 (clarification #2)", code)
	}
	if !strings.Contains(stdout, "up to date") {
		t.Errorf("stdout does not state the no-op reason:\n%s", stdout)
	}
	if strings.Contains(stdout, "updated") {
		t.Errorf("no-op claims an update occurred:\n%s", stdout)
	}
	if string(lockBefore) != string(readFile(t, filepath.Join(proj, "skills-lock.json"))) {
		t.Error("no-op named update modified the lockfile")
	}
}

func TestUpdateNamed_UnknownSkillFails(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	local := localSkillDir(t, "present")
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", local); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if _, _, code := runGskill(t, proj, "update", "nope"); code == 0 {
		t.Error("unknown skill name exited 0, want non-zero")
	}
}

func TestUpdate_PartialFailureContinuesAndExits10(t *testing.T) {
	t.Parallel()

	repoA := gitRepo(t, validSkill("alpha"), "v1.0.0")
	repoB := gitRepo(t, validSkill("beta"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	for _, repo := range []string{repoA, repoB} {
		if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
			t.Fatalf("add: %s", stderr)
		}
	}
	publishNewVersion(t, repoA, "alpha", "v1.1.0")
	publishNewVersion(t, repoB, "beta", "v1.1.0")
	// Break beta's source after discovery would still see both updates.
	if err := os.RemoveAll(repoB); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runGskill(t, proj, "update")
	if code != 10 {
		t.Errorf("partial failure exit = %d, want 10 (FR-020)", code)
	}
	if !strings.Contains(stdout, "failed") {
		t.Errorf("stdout missing the failed outcome:\n%s", stdout)
	}

	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.1.0"`) {
		t.Errorf("surviving skill did not update:\n%s", lock)
	}
	if !strings.Contains(lock, `"version": "1.0.0"`) {
		t.Errorf("failed skill left partially updated:\n%s", lock)
	}
}

func TestUpdate_NoInteractiveUpdatesAllActionableWithoutPrompt(t *testing.T) {
	t.Parallel()

	repoA := gitRepo(t, validSkill("alpha"), "v1.0.0")
	repoB := gitRepo(t, validSkill("beta"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	for _, repo := range []string{repoA, repoB} {
		if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
			t.Fatalf("add: %s", stderr)
		}
	}
	publishNewVersion(t, repoA, "alpha", "v1.1.0")
	publishNewVersion(t, repoB, "beta", "v1.1.0")

	// The harness runs with buffer streams (no TTY), which is exactly the
	// piped case; --no-interactive makes the intent explicit. Neither may
	// prompt (FR-012) — an in-process prompt would hang, so completing at
	// all with both skills updated is the assertion.
	stdout, stderr, code := runGskill(t, proj, "--no-interactive", "update")
	if code != 0 {
		t.Fatalf("--no-interactive update exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "alpha") || !strings.Contains(stdout, "beta") {
		t.Errorf("stdout missing per-skill rows:\n%s", stdout)
	}
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if strings.Contains(lock, `"version": "1.0.0"`) {
		t.Errorf("an actionable skill was not updated:\n%s", lock)
	}
}

// jsonUpdateProject builds a project with one actionable skill (demo,
// locked 1.0.0, 1.1.0 published) for JSON-contract tests.
func jsonUpdateProject(t *testing.T) string {
	t.Helper()
	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")
	return proj
}

// assertSingleJSON decodes stdout as exactly one JSON object (SC-005).
func assertSingleJSON(t *testing.T, stdout, label string) map[string]any {
	t.Helper()
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("%s stdout is not JSON (SC-005): %v\n%s", label, err, stdout)
	}
	if dec.More() {
		t.Errorf("%s stdout has more than one JSON value:\n%s", label, stdout)
	}
	return obj
}

func TestUpdateList_JSONSingleObject(t *testing.T) {
	t.Parallel()

	proj := jsonUpdateProject(t)
	stdout, _, code := runGskill(t, proj, "--json", "update", "--list")
	if code != 0 {
		t.Fatalf("--json update --list exit = %d", code)
	}
	obj := assertSingleJSON(t, stdout, "update --list")
	if obj["updates_available"] != float64(1) {
		t.Errorf("updates_available = %v, want 1:\n%s", obj["updates_available"], stdout)
	}
	// not_checked lets automation distinguish "verified current" from
	// "could not be determined" (review round 2).
	if obj["not_checked"] != float64(0) {
		t.Errorf("not_checked = %v, want 0:\n%s", obj["not_checked"], stdout)
	}
}

func TestUpdate_JSONSingleObjectWithCleanStdout(t *testing.T) {
	t.Parallel()

	proj := jsonUpdateProject(t)
	stdout, _, code := runGskill(t, proj, "--json", "update")
	if code != 0 {
		t.Fatalf("--json update exit = %d", code)
	}
	obj := assertSingleJSON(t, stdout, "update")
	if obj["changed"] != true {
		t.Errorf("changed = %v, want true:\n%s", obj["changed"], stdout)
	}
	skills, ok := obj["skills"].([]any)
	if !ok || len(skills) != 1 {
		t.Fatalf("skills = %v, want one row", obj["skills"])
	}
	row, castOK := skills[0].(map[string]any)
	if !castOK {
		t.Fatalf("skill row has unexpected shape: %v", skills[0])
	}
	// Historical fields retained, transition fields added (FR-016).
	for _, field := range []string{"name", "content_hash", "changed", "from", "to", "status", "outcome"} {
		if _, present := row[field]; !present {
			t.Errorf("skill row missing %q:\n%s", field, stdout)
		}
	}
	if row["from"] != "1.0.0" || row["to"] != "1.1.0" {
		t.Errorf("from/to = %v/%v, want 1.0.0/1.1.0", row["from"], row["to"])
	}
}

func TestUpdate_DryRunWritesNothing(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")

	lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))
	activeBefore := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))

	stdout, _, code := runGskill(t, proj, "--dry-run", "update")
	if code != 0 {
		t.Fatalf("--dry-run update exit = %d, want 0", code)
	}
	if !regexp.MustCompile(`demo\s+1\.0\.0\s+1\.1\.0\s+would update`).MatchString(stdout) {
		t.Errorf("dry-run missing would-update transition:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Dry run: no changes made.") {
		t.Errorf("dry-run missing notice:\n%s", stdout)
	}

	if string(lockBefore) != string(readFile(t, filepath.Join(proj, "skills-lock.json"))) {
		t.Error("--dry-run modified skills-lock.json (FR-014)")
	}
	if string(activeBefore) != string(readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))) {
		t.Error("--dry-run modified active skill content (FR-014)")
	}

	// Named dry-run behaves identically.
	if _, _, namedCode := runGskill(t, proj, "--dry-run", "update", "demo"); namedCode != 0 {
		t.Errorf("--dry-run update demo exit = %d, want 0", namedCode)
	}
	if string(lockBefore) != string(readFile(t, filepath.Join(proj, "skills-lock.json"))) {
		t.Error("named --dry-run modified skills-lock.json")
	}
}

func TestUpdate_OfflineMakesNoNetworkCalls(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	counting := &testutil.CountingGit{Inner: git.NewSystemRunner()}
	a := app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     discardLogger(),
		Git:        counting,
		GskillHome: filepath.Join(t.TempDir(), "home"),
	})
	if _, stderr, code := runGskillWithApp(t, a, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")

	before := counting.ResolutionCalls()
	listOut, _, code := runGskillWithApp(t, a, proj, "--offline", "update", "--list", "--all")
	if code != 0 {
		t.Fatal("--offline update --list --all failed")
	}
	// An offline run must never claim freshness it did not verify: the
	// mutable-policy skill is reported unchecked, not up to date.
	if !strings.Contains(listOut, "not checked") || !strings.Contains(listOut, "could not be checked") {
		t.Errorf("offline list hides the unchecked state:\n%s", listOut)
	}
	stdout, _, code := runGskillWithApp(t, a, proj, "--offline", "--no-interactive", "update")
	if code != 0 {
		t.Fatalf("--offline update exit = %d:\n%s", code, stdout)
	}
	if strings.Contains(stdout, "All skills are up to date") {
		t.Errorf("offline update claims up-to-date without checking:\n%s", stdout)
	}
	if !strings.Contains(stdout, "not checked") {
		t.Errorf("offline update hides the unchecked skill:\n%s", stdout)
	}
	if got := counting.ResolutionCalls(); got != before {
		t.Errorf("offline update made %d network calls (FR-014)", got-before)
	}
	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.0.0"`) {
		t.Errorf("offline update changed the lock:\n%s", lock)
	}
}

func TestUpdateList_UsageErrors(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, _, code := runGskill(t, proj, "update", "--list", "some-skill"); code == 0 {
		t.Error("update --list with names must be a usage error")
	}
	if _, _, code := runGskill(t, proj, "update", "--all"); code == 0 {
		t.Error("update --all without --list must be a usage error")
	}
}

func TestUpdate_AdvancesLockWithinConstraint(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	if _, stderr, code := runGskill(t, proj, "init"); code != 0 {
		t.Fatalf("init: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	lock := string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.0.0"`) {
		t.Fatalf("initial lock not at 1.0.0:\n%s", lock)
	}

	// Publish a newer in-constraint version on a new commit.
	if err := os.WriteFile(filepath.Join(repo, "demo", "SKILL.md"),
		[]byte("---\nname: demo\ndescription: updated demo\n---\n# demo v1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "--quiet", "-m", "v1.1")
	gitRun(t, repo, "tag", "v1.1.0")

	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}

	lock = string(readFile(t, filepath.Join(proj, "skills-lock.json")))
	if !strings.Contains(lock, `"version": "1.1.0"`) {
		t.Errorf("lock did not advance to 1.1.0:\n%s", lock)
	}
}
