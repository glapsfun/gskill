package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
