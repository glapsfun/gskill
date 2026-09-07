package integration_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/testutil"
)

// upgradeProject declares demo at ^1.0.0 (locked 1.0.0) and publishes 1.1.0
// and 2.0.0, so update can move within 1.x and only upgrade reaches 2.x.
func upgradeProject(t *testing.T) (proj, repo string) {
	t.Helper()
	repo = gitRepo(t, validSkill("demo"), "v1.0.0")
	proj = newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")
	publishNewVersion(t, repo, "demo", "v2.0.0")
	return proj, repo
}

func changedLines(a, b string) []string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(al) != len(bl) {
		return []string{"(line count differs)"}
	}
	var out []string
	for i := range al {
		if al[i] != bl[i] {
			out = append(out, bl[i])
		}
	}
	return out
}

func TestUpgrade_LatestRewritesOneLine(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	before := string(readFile(t, manifestPath(proj)))
	if _, stderr, code := runGskill(t, proj, "update"); code != 0 {
		t.Fatalf("update: %s", stderr)
	}
	if !strings.Contains(readLock(t, proj), `"version": "1.1.0"`) {
		t.Fatalf("update should stay within ^1.0.0:\n%s", readLock(t, proj))
	}
	stdout, stderr, code := runGskill(t, proj, "upgrade", "demo", "--latest")
	if code != 0 {
		t.Fatalf("upgrade exit %d: %s\n%s", code, stderr, stdout)
	}
	after := string(readFile(t, manifestPath(proj)))
	if diff := changedLines(before, after); len(diff) != 1 || !strings.Contains(diff[0], `version = "^2.0.0"`) {
		t.Fatalf("manifest diff = %v, want exactly one line with ^2.0.0\n%s", diff, after)
	}
	lock := readLock(t, proj)
	if !strings.Contains(lock, `"version": "2.0.0"`) || !strings.Contains(lock, `"requestedVersion": "^2.0.0"`) {
		t.Errorf("lock not at 2.0.0 under ^2.0.0:\n%s", lock)
	}
	if got := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")); !strings.Contains(string(got), "v2.0.0") {
		t.Errorf("installed content not at 2.0.0:\n%s", got)
	}
	for _, want := range []string{"upgraded", "^1.0.0", "^2.0.0", "verified"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if _, stderr, code := runGskill(t, proj, "verify"); code != 0 {
		t.Errorf("verify after upgrade: %s", stderr)
	}
}

func TestUpgrade_ToExplicitVersionVerbatim(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "1.1.0"); code != 0 {
		t.Fatalf("upgrade: %s", stderr)
	}
	m := string(readFile(t, manifestPath(proj)))
	if !strings.Contains(m, `version = "^1.1.0"`) {
		t.Errorf("caret shape not preserved at the explicit version:\n%s", m)
	}
	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "--to", "2.0.0"); code != 0 {
		t.Fatalf("upgrade --to: %s", stderr)
	}
	if m := string(readFile(t, manifestPath(proj))); !strings.Contains(m, `version = "^2.0.0"`) {
		t.Errorf("--to not honoured:\n%s", m)
	}
}

func TestUpgrade_ExactStaysExact(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v2.0.0")
	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "--latest"); code != 0 {
		t.Fatalf("upgrade: %s", stderr)
	}
	m := string(readFile(t, manifestPath(proj)))
	if !strings.Contains(m, `version = "2.0.0"`) || strings.Contains(m, "^") {
		t.Errorf("exact pin became something else:\n%s", m)
	}
	if !strings.Contains(readLock(t, proj), `"declarationKind": "exact-version"`) {
		t.Errorf("lock kind not recorded:\n%s", readLock(t, proj))
	}
}

func TestUpgrade_CandidateWithinRangeIsUpdateOnly(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v1.1.0")
	before := readFile(t, manifestPath(proj))
	stdout, stderr, code := runGskill(t, proj, "upgrade", "demo")
	if code != 0 {
		t.Fatalf("upgrade: %s", stderr)
	}
	if !bytes.Equal(before, readFile(t, manifestPath(proj))) {
		t.Errorf("manifest rewritten although 1.1.0 satisfies ^1.0.0")
	}
	if !strings.Contains(readLock(t, proj), `"version": "1.1.0"`) || !strings.Contains(stdout, "updated") {
		t.Errorf("lock did not move / result not 'updated':\n%s\n%s", readLock(t, proj), stdout)
	}
}

func TestUpgrade_RefusesBranchLocalAndOtherRange(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	local := localSkillDir(t, "loc")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--ref", "main"); code != 0 {
		t.Fatalf("add branch: %s", stderr)
	}
	if _, stderr, code := runGskill(t, proj, "add", local); code != 0 {
		t.Fatalf("add local: %s", stderr)
	}
	manifest, lock := readFile(t, manifestPath(proj)), readLock(t, proj)
	for _, name := range []string{"demo", "loc"} {
		_, stderr, code := runGskill(t, proj, "upgrade", name)
		if code != 2 {
			t.Errorf("upgrade %s exit = %d, want 2: %s", name, code, stderr)
		}
		if !strings.Contains(stderr, "hint") && !strings.Contains(stderr, "gskill") {
			t.Errorf("upgrade %s stderr lacks guidance: %s", name, stderr)
		}
	}
	setManifestVersion := func(from, to string) {
		data := readFile(t, manifestPath(proj))
		if err := os.WriteFile(manifestPath(proj), bytes.Replace(data, []byte(from), []byte(to), 1), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	setManifestVersion(`ref = "main"`, `version = ">=1.0.0 <3.0.0"`)
	if _, stderr, code := runGskill(t, proj, "upgrade", "demo"); code != 2 || !strings.Contains(stderr, "edit skills.toml") {
		t.Errorf("range-other: exit %d stderr %s", code, stderr)
	}
	setManifestVersion(`version = ">=1.0.0 <3.0.0"`, `ref = "main"`)
	if !bytes.Equal(manifest, readFile(t, manifestPath(proj))) || lock != readLock(t, proj) {
		t.Error("a refused upgrade must write nothing")
	}
}

func TestUpgrade_DowngradeLabelled(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "--latest"); code != 0 {
		t.Fatalf("upgrade: %s", stderr)
	}
	stdout, stderr, code := runGskill(t, proj, "upgrade", "demo", "--to", "1.1.0")
	if code != 0 {
		t.Fatalf("downgrade: %s", stderr)
	}
	if !strings.Contains(stdout, "downgraded") || !strings.Contains(readLock(t, proj), `"version": "1.1.0"`) {
		t.Errorf("downgrade not labelled/applied:\n%s\n%s", stdout, readLock(t, proj))
	}
}

func TestUpgrade_NonInteractiveNoNamesRequiresAll(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	before := readFile(t, manifestPath(proj))
	if _, stderr, code := runGskill(t, proj, "--no-interactive", "upgrade"); code != 2 || !strings.Contains(stderr, "--all") {
		t.Errorf("exit %d stderr %s, want 2 with --all guidance", code, stderr)
	}
	if !bytes.Equal(before, readFile(t, manifestPath(proj))) {
		t.Error("manifest changed without --all")
	}
	if _, stderr, code := runGskill(t, proj, "--no-interactive", "upgrade", "--all"); code != 0 {
		t.Fatalf("upgrade --all: %s", stderr)
	}
	if !strings.Contains(string(readFile(t, manifestPath(proj))), `version = "^2.0.0"`) {
		t.Error("--all did not upgrade")
	}
}

func TestUpgrade_DryRunWritesNothing(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	manifest, lock := readFile(t, manifestPath(proj)), readLock(t, proj)
	stdout, stderr, code := runGskill(t, proj, "--dry-run", "upgrade", "demo")
	if code != 0 {
		t.Fatalf("dry run: %s", stderr)
	}
	for _, want := range []string{"would upgrade", "^2.0.0", "Dry run"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if !bytes.Equal(manifest, readFile(t, manifestPath(proj))) || lock != readLock(t, proj) {
		t.Error("dry run wrote something")
	}
}

func TestUpgrade_JSONSingleObject(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	stdout, stderr, code := runGskill(t, proj, "--json", "upgrade", "demo")
	if code != 0 {
		t.Fatalf("upgrade --json: %s", stderr)
	}
	obj := assertSingleJSON(t, stdout, "upgrade")
	skills, _ := obj["skills"].([]any)
	if len(skills) != 1 || obj["upgraded"] != float64(1) {
		t.Fatalf("obj = %v", obj)
	}
	item, _ := skills[0].(map[string]any)
	if item["declaration_after"] != `version = "^2.0.0"` || item["outcome"] != "upgraded" || item["shape"] != "range-caret" {
		t.Errorf("item = %v", item)
	}
}

func TestUpgrade_OfflineRefusesExit2(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	before := readFile(t, manifestPath(proj))
	if _, stderr, code := runGskill(t, proj, "--offline", "upgrade", "demo"); code != 2 || !strings.Contains(stderr, "offline") {
		t.Errorf("exit %d stderr %s", code, stderr)
	}
	if !bytes.Equal(before, readFile(t, manifestPath(proj))) {
		t.Error("offline upgrade wrote the manifest")
	}
}

func TestUpgrade_UnknownVersionRefusedNothingWritten(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	manifest, lock := readFile(t, manifestPath(proj)), readLock(t, proj)
	content := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))
	_, stderr, code := runGskill(t, proj, "upgrade", "demo", "--to", "9.9.9")
	if code != 2 || !strings.Contains(stderr, "9.9.9") {
		t.Errorf("exit %d stderr %s", code, stderr)
	}
	if !bytes.Equal(manifest, readFile(t, manifestPath(proj))) || lock != readLock(t, proj) ||
		!bytes.Equal(content, readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))) {
		t.Error("a refused upgrade changed a layer")
	}
}

func TestUpgrade_AllSkipsForeignEntries(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	lockPath := filepath.Join(proj, "skills-lock.json")
	data := readFile(t, lockPath)
	foreign := []byte(`"skills": {
    "other-tool-skill": {"source": "github:someone/else", "sourceType": "git", "skillPath": "x", "computedHash": "sha256:cccc", "otherTool": {"itsOwnField": "must survive"}},`)
	data = bytes.Replace(data, []byte(`"skills": {`), foreign, 1)
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runGskill(t, proj, "--no-interactive", "upgrade", "--all")
	if code != 0 {
		t.Fatalf("upgrade --all: %s", stderr)
	}
	if strings.Contains(stdout, "other-tool-skill") {
		t.Errorf("foreign entry in the report:\n%s", stdout)
	}
	if lock := readLock(t, proj); !strings.Contains(lock, `"itsOwnField": "must survive"`) || !strings.Contains(lock, `"version": "2.0.0"`) {
		t.Errorf("foreign entry damaged or upgrade missed:\n%s", lock)
	}
}

func TestUpgrade_CommitShapeWithoutReleaseRefused(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"))
	sha := testutil.GitOutput(t, repo, "rev-parse", "HEAD")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--commit", sha); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	stdout, stderr, code := runGskill(t, proj, "upgrade", "demo", "--latest")
	if code != 0 || !strings.Contains(stdout, "unchanged") {
		t.Errorf("a commit pin with no releases is an honest no-op: exit %d\n%s%s", code, stdout, stderr)
	}
	next := testutil.PublishCommit(t, repo, "demo", testutil.SkillBody("demo", "next"))
	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "--to", next); code != 0 {
		t.Fatalf("upgrade --to commit: %s", stderr)
	}
	if !strings.Contains(string(readFile(t, manifestPath(proj))), `commit = "`+next+`"`) {
		t.Error("commit pin not moved")
	}
}

func TestUpgrade_PreManifestProjectGeneratesFirst(t *testing.T) {
	t.Parallel()

	proj, _ := upgradeProject(t)
	if err := os.Remove(manifestPath(proj)); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runGskill(t, proj, "upgrade", "demo")
	if code != 0 {
		t.Fatalf("exit %d stderr %s", code, stderr)
	}
	// The manifest is generated from the lock first (the notice itself goes
	// to the App's notice writer), then upgraded in the same run.
	if !strings.Contains(string(readFile(t, manifestPath(proj))), `version = "^2.0.0"`) {
		t.Errorf("generated manifest not upgraded:\n%s", readFile(t, manifestPath(proj)))
	}
}

// TestUpgrade_RollbackOnInstallFailure is spec 024 FR-014: a failure after the
// manifest was rewritten restores every layer.
func TestUpgrade_RollbackOnInstallFailure(t *testing.T) { //nolint:paralleltest // mutates a package-level test seam
	proj, _ := upgradeProject(t)
	manifest, lock := readFile(t, manifestPath(proj)), readLock(t, proj)
	content := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))
	link := readFile(t, filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md"))

	restore := app.SetUpgradeFailureSeam(func() error { return errors.New("injected failure after manifest write") })
	defer restore()

	stdout, stderr, code := runGskill(t, proj, "upgrade", "demo")
	if code == 0 {
		t.Fatalf("upgrade succeeded despite the injected failure:\n%s", stdout)
	}
	if !strings.Contains(stderr+stdout, "injected failure") {
		t.Errorf("failure not reported:\n%s%s", stdout, stderr)
	}
	if !bytes.Equal(manifest, readFile(t, manifestPath(proj))) {
		t.Errorf("manifest not restored:\n%s", readFile(t, manifestPath(proj)))
	}
	if lock != readLock(t, proj) {
		t.Error("lock not restored")
	}
	if !bytes.Equal(content, readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))) ||
		!bytes.Equal(link, readFile(t, filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md"))) {
		t.Error("installed content not restored")
	}
}
