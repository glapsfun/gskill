package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/testutil"
)

// setManifestKey rewrites one scalar key in a skill's manifest block.
func setManifestKey(t *testing.T, proj, name, key, value string) {
	t.Helper()
	data, err := os.ReadFile(manifestPath(proj))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	inBlock, wrote := false, false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "[") {
			inBlock = trimmed == "[skills."+name+"]"
			continue
		}
		if inBlock && strings.HasPrefix(trimmed, key+" ") {
			lines[i] = key + ` = "` + value + `"`
			wrote = true
		}
	}
	if !wrote {
		// Append into the block when the key was not already present.
		for i, l := range lines {
			if strings.TrimSpace(l) == "[skills."+name+"]" {
				lines = append(lines[:i+1], append([]string{key + ` = "` + value + `"`}, lines[i+1:]...)...)
				break
			}
		}
	}
	if err := os.WriteFile(manifestPath(proj), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestPin_ManifestRefSelectsRevision is FR-003/FR-004: pinning acts at
// resolution time. Editing the declared ref must move the installed skill,
// otherwise one of the four advertised override kinds silently does nothing.
func TestPin_ManifestRefSelectsRevision(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	publishNewVersion(t, repo, "demo", "v1.1.0")

	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Re-declare the skill at the newer tag.
	setManifestKey(t, proj, "demo", "version", "1.1.0")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}

	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")) //nolint:gosec // test project
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "v1.1.0") {
		t.Errorf("declared pin did not select the revision:\n%s", content)
	}
	if lock := readLock(t, proj); !strings.Contains(lock, "1.1.0") {
		t.Errorf("lock did not record the newly pinned revision:\n%s", lock)
	}
}

// TestPin_EditConverges: after a declared pin is installed, the lock must
// record that pin as the intent. Recording the prior intent instead leaves the
// two halves disagreeing forever — every later install re-resolves over the
// network and rewrites the entry rather than reporting it up to date, and
// update advances within the constraint the user replaced.
func TestPin_EditConverges(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	publishNewVersion(t, repo, "demo", "v1.1.0")

	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	setManifestKey(t, proj, "demo", "version", "1.1.0")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	settled := readLock(t, proj)

	// The declared pin is now the recorded intent, so a second run changes
	// nothing at all.
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("second install: %s", stderr)
	}
	if after := readLock(t, proj); after != settled {
		t.Errorf("install after a pin edit never converges\n--- first ---\n%s\n--- second ---\n%s", settled, after)
	}
	// The recorded *intent* must be the declared pin, not the prior one. This
	// is the fact that makes the run converge: comparing lock bytes alone
	// passes even when intent is stale, because re-resolving the same pin
	// reproduces identical output while still hitting the network every run.
	if !strings.Contains(settled, `"requestedVersion": "1.1.0"`) {
		t.Errorf("lock recorded stale intent after a pin edit:\n%s", settled)
	}
	if strings.Contains(settled, `"requestedVersion": "1.0.0"`) {
		t.Errorf("lock still records the replaced constraint:\n%s", settled)
	}
}

// TestPin_RangeWidenReResolves is spec 024 FR-002: widening a range in
// skills.toml re-resolves on the next install.
func TestPin_RangeWidenReResolves(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	publishNewVersion(t, repo, "demo", "v2.0.0")
	setManifestKey(t, proj, "demo", "version", "^2.0.0")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	lock := readLock(t, proj)
	if !strings.Contains(lock, `"version": "2.0.0"`) || !strings.Contains(lock, `"requestedVersion": "^2.0.0"`) {
		t.Errorf("widened range not re-resolved:\n%s", lock)
	}
}

// TestPin_SourceEditReResolves: editing `source` re-resolves from the new
// source and the lock records it.
func TestPin_SourceEditReResolves(t *testing.T) {
	t.Parallel()

	repoA := gitRepo(t, validSkill("demo"), "v1.0.0")
	repoB := gitRepo(t, "---\nname: demo\ndescription: from B\n---\n# demo from-B\n", "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repoA, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	setManifestKey(t, proj, "demo", "source", repoB)
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	content := readFile(t, filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md"))
	if !strings.Contains(string(content), "from-B") {
		t.Errorf("source edit ignored:\n%s", content)
	}
	if lock := readLock(t, proj); !strings.Contains(lock, repoB) {
		t.Errorf("lock does not record the new source:\n%s", lock)
	}
}

// TestPin_SkillPathEditReResolves: editing `skill` is read on install — a
// path the source does not have fails the run instead of being ignored.
func TestPin_SkillPathEditReResolves(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	lock := readLock(t, proj)
	setManifestKey(t, proj, "demo", "skill", "missing/path")
	if _, stderr, code := runGskill(t, proj, "install"); code == 0 || !strings.Contains(stderr, "missing/path") {
		t.Errorf("edited skill path silently ignored: exit %d, stderr %s", code, stderr)
	}
	if readLock(t, proj) != lock {
		t.Error("a failed re-resolution rewrote the lock")
	}
}

// TestPin_ModeEditRematerializes: switching mode to copy re-materializes the
// agent target as a real directory.
func TestPin_ModeEditRematerializes(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	target := filepath.Join(proj, ".claude", "skills", "demo")
	if fi, err := os.Lstat(target); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected a symlink before the edit (err %v)", err)
	}
	setManifestKey(t, proj, "demo", "mode", "copy")
	if _, stderr, code := runGskill(t, proj, "install"); code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	if fi, err := os.Lstat(target); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("mode edit ignored: target still a symlink (err %v)", err)
	}
	if !strings.Contains(readLock(t, proj), `"installMode": "copy"`) {
		t.Errorf("lock does not record the copy mode:\n%s", readLock(t, proj))
	}
}

// TestPin_SkillPathEditToInstalledNameReResolves: a skill installed as "demo"
// from "legacy/demo" is re-pointed by editing `skill` to "demo". The runtime
// honours that key as a source path, so this is a real change. The diff used
// to exempt any value equal to the installed name, so install short-circuited
// to "up to date" and kept serving legacy/demo — and --frozen-lockfile
// accepted the mismatch, so CI could not see it either.
func TestPin_SkillPathEditToInstalledNameReResolves(t *testing.T) {
	t.Parallel()

	repo := testutil.InitSkillRepo(t, "legacy/demo", validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--skill", "demo", "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if data := readFile(t, manifestPath(proj)); !bytes.Contains(data, []byte(`skill = "legacy/demo"`)) {
		t.Fatalf("manifest does not record the source path:\n%s", data)
	}
	lock := readLock(t, proj)

	setManifestKey(t, proj, "demo", "skill", "demo")
	_, stderr, code := runGskill(t, proj, "install")
	if code == 0 {
		t.Errorf("install ignored the changed skill path and reported success; stderr %s", stderr)
	}
	if readLock(t, proj) != lock {
		t.Error("a failed re-resolution rewrote the lock")
	}
	if _, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile"); code != 4 {
		t.Errorf("frozen install with a changed skill path exit = %d, want 4; stderr %s", code, stderr)
	}
}
