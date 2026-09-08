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

// TestUpgrade_NonSemverTargetRefusedForVersionShape: a repository may carry
// tags that are not semver. Targeting one from an exact version pin cannot be
// expressed as a version, so the upgrade must refuse and write nothing. Before
// the guard it wrote `version = ""`, erasing the pin, and the same run then
// installed whatever was latest and reported success.
func TestUpgrade_NonSemverTargetRefusedForVersionShape(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v2.0.0")
	testutil.PublishVersion(t, repo, "demo",
		"---\nname: demo\ndescription: updated\n---\n# demo release-x\n", "release-x")
	manifest, lock := readFile(t, manifestPath(proj)), readLock(t, proj)

	_, stderr, code := runGskill(t, proj, "upgrade", "demo", "--to", "release-x")
	if code == 0 {
		t.Fatalf("upgrade to a non-semver tag succeeded, want refusal; stderr %s", stderr)
	}
	after := readFile(t, manifestPath(proj))
	if bytes.Contains(after, []byte(`version = ""`)) {
		t.Errorf("the exact pin was erased:\n%s", after)
	}
	if !bytes.Equal(manifest, after) {
		t.Errorf("refused upgrade rewrote skills.toml:\n%s", after)
	}
	if lock != readLock(t, proj) {
		t.Error("refused upgrade rewrote skills-lock.json")
	}
}

// TestUpgrade_NarrowedAgentsRemovesTarget: upgrade reaches the same
// installOne path as update, so it must also remove the target of an agent
// the manifest no longer declares.
func TestUpgrade_NarrowedAgentsRemovesTarget(t *testing.T) {
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
	publishNewVersion(t, repo, "demo", "v2.0.0")

	data := readFile(t, manifestPath(proj))
	out := bytes.Replace(data, []byte(`agents = ["claude", "codex"]`), []byte(`agents = ["claude"]`), 1)
	if bytes.Equal(data, out) {
		t.Fatalf("could not narrow the agent list in:\n%s", data)
	}
	if err := os.WriteFile(manifestPath(proj), out, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "--latest"); code != 0 {
		t.Fatalf("upgrade: %s", stderr)
	}
	if _, err := os.Stat(codexTarget); err == nil {
		t.Error("upgrade left the dropped agent's target behind as an untracked orphan")
	}
}

// TestUpgrade_SourceEditDiscoversFromManifest: after the source is re-pointed
// in skills.toml, releases must be discovered from the repository the install
// will use. Discovery used to read the lock's source, so a release that exists
// only in the new repository was refused as non-existent, and --latest could
// pick a release from the abandoned one.
func TestUpgrade_SourceEditDiscoversFromManifest(t *testing.T) {
	t.Parallel()

	repoA := gitRepo(t, validSkill("demo"), "v1.0.0")
	repoB := gitRepo(t, "---\nname: demo\ndescription: from B\n---\n# demo from-B v1.0.0\n", "v1.0.0")
	testutil.PublishVersion(t, repoB, "demo",
		"---\nname: demo\ndescription: from B\n---\n# demo from-B v2.0.0\n", "v2.0.0")

	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repoA, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	setManifestKey(t, proj, "demo", "source", repoB)

	if _, stderr, code := runGskill(t, proj, "upgrade", "demo", "--to", "2.0.0"); code != 0 {
		t.Fatalf("upgrade to a release that exists only in the declared source: %s", stderr)
	}
	content := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))
	if !strings.Contains(string(content), "from-B v2.0.0") {
		t.Errorf("installed content is not the declared source's 2.0.0:\n%s", content)
	}
}

// TestUpgrade_RollbackAfterActivation is the failure the earlier rollback test
// cannot reach: the seam interrupts the *second* skill, so the first has
// already been installed and activated. Rolling back must put its content back
// too. It used to reconcile from the restored lock without replace semantics,
// so the installer classified the upgraded directory as drift and refused to
// touch it — the manifest and lock went back while the agents kept reading the
// upgraded skill, and the failure was only written to the debug log.
func TestUpgrade_RollbackAfterActivation(t *testing.T) { //nolint:paralleltest // mutates a package-level test seam
	repo := testutil.InitSkillRepo(t, "alpha", validSkill("alpha"))
	testutil.PublishCommit(t, repo, "beta", validSkill("beta"))
	testutil.GitRun(t, repo, "tag", "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--all", "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	testutil.PublishVersion(t, repo, "alpha", "---\nname: alpha\ndescription: a demo skill\n---\n# alpha v2\n", "v2.0.0")

	manifest, lock := readFile(t, manifestPath(proj)), readLock(t, proj)
	alphaPath := filepath.Join(proj, ".agents", "skills", "alpha", "SKILL.md")
	alphaBefore := readFile(t, alphaPath)

	var calls int
	restore := app.SetUpgradeFailureSeam(func() error {
		calls++
		if calls == 1 {
			return nil // let the first skill install and activate
		}
		return errors.New("injected failure after the first activation")
	})
	defer restore()

	stdout, _, code := runGskill(t, proj, "--no-interactive", "upgrade", "--all")
	if code == 0 {
		t.Fatalf("upgrade succeeded despite the injected failure:\n%s", stdout)
	}
	if calls < 2 {
		t.Fatalf("seam fired %d times, want at least 2 — the first skill never activated", calls)
	}
	if !bytes.Equal(manifest, readFile(t, manifestPath(proj))) {
		t.Errorf("manifest not restored:\n%s", readFile(t, manifestPath(proj)))
	}
	if lock != readLock(t, proj) {
		t.Error("lock not restored")
	}
	if got := readFile(t, alphaPath); !bytes.Equal(alphaBefore, got) {
		t.Errorf("installed content not restored — the agents still read the upgraded skill:\n%s", got)
	}
	// --fail-on-drift is what makes this load-bearing: a plain check exits 0
	// even with upgraded content sitting under a reverted lock.
	if _, checkErr, checkCode := runGskill(t, proj, "check", "--fail-on-drift"); checkCode != 0 {
		t.Errorf("check --fail-on-drift after rollback: exit %d %s", checkCode, checkErr)
	}
}

// TestUpgrade_RollbackPreservesHandEditedContent: a hand-edited committed copy
// makes the upgrade fail closed, which is correct — but the rollback must not
// then destroy the edit it just refused to overwrite. The rollback may replace
// only content this run installed itself; anything else is the user's, and a
// restore that cannot proceed is reported rather than forced.
func TestUpgrade_RollbackPreservesHandEditedContent(t *testing.T) {
	t.Parallel()

	proj, repo := upgradeProject(t)
	publishNewVersion(t, repo, "demo", "v3.0.0")
	committed := filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")
	edited := "---\nname: demo\ndescription: a demo skill\n---\n# demo (hand edited)\n"
	if err := os.WriteFile(committed, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, lock := readFile(t, manifestPath(proj)), readLock(t, proj)

	_, stderr, code := runGskill(t, proj, "upgrade", "demo", "--latest")
	if code == 0 {
		t.Fatalf("upgrade over drifted committed content succeeded, want fail-closed; stderr %s", stderr)
	}
	if got := string(readFile(t, committed)); got != edited {
		t.Errorf("the rollback destroyed hand-edited content:\n%s", got)
	}
	if !bytes.Equal(manifest, readFile(t, manifestPath(proj))) {
		t.Error("manifest not restored")
	}
	if lock != readLock(t, proj) {
		t.Error("lock not restored")
	}
}
