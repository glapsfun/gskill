package integration_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantLegacySource rewrites the source a project declares and the lock
// records from current to legacy, the spelling an earlier release accepted,
// while the lock keeps the clone address, owner, repo, and path it resolved.
func plantLegacySource(t *testing.T, proj, current, legacy string) {
	t.Helper()
	for _, f := range []string{"skills.toml", "skills-lock.json"} {
		p := filepath.Join(proj, f)
		data := readFile(t, p)
		if !bytes.Contains(data, []byte(`"`+current+`"`)) {
			t.Fatalf("%s does not record %q:\n%s", f, current, data)
		}
		if err := os.WriteFile(p, bytes.ReplaceAll(data, []byte(`"`+current+`"`), []byte(`"`+legacy+`"`)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLegacyLock_AmbiguousGitLabSourceKeepsItsBoundary: a project that added
// gitlab.com/acme/skills/tools/lint before the GitLab ambiguity rule keeps
// installing, updating, grouping, and listing versions from the repository
// and folder its lock recorded (spec 035 FR-015, SC-005).
func TestLegacyLock_AmbiguousGitLabSourceKeepsItsBoundary(t *testing.T) { //nolint:paralleltest // sets GIT_CONFIG_*
	const legacy = "gitlab.com/acme/skills/tools/lint"
	fixture := providerFixture(t)
	rewriteRemotes(t, fixture, "https://gitlab.com/acme/skills.git")
	a := noAPIApp(t)
	proj := newProject(t)
	runOK(t, a, proj, "add", "gitlab.com/acme/skills//tools/lint", "--ref", "v2.0.0", "--agent", "claude")
	plantLegacySource(t, proj, "gitlab.com/acme/skills//tools/lint", legacy)
	before := lockEntryOf(t, proj, "lint")
	lockBytes := readFile(t, filepath.Join(proj, "skills-lock.json"))

	teammate := freshCopy(t, proj)
	runOK(t, noAPIApp(t), teammate, "install", "--frozen-lockfile")
	runOK(t, a, proj, "install")
	if got := readFile(t, filepath.Join(proj, "skills-lock.json")); !bytes.Equal(got, lockBytes) {
		t.Errorf("a no-op install rewrote the legacy lockfile:\n%s", got)
	}
	runOK(t, a, proj, "update")
	after := lockEntryOf(t, proj, "lint")
	if after.Gskill.Commit != before.Gskill.Commit || after.Gskill.ContentHash != before.Gskill.ContentHash {
		t.Errorf("update moved the legacy entry: %+v -> %+v", before.Gskill, after.Gskill)
	}

	stdout, stderr, code := runGskillWithApp(t, a, proj, "--no-interactive", "--json", "update", "--list", "--repo", "gitlab.com/acme/skills")
	// lint is pinned by tag, so the repository group selects it as pinned.
	if code != 0 || !strings.Contains(stdout, `"pinned": 1`) {
		t.Errorf("update --list --repo gitlab.com/acme/skills: exit %d, lint not selected\n%s%s", code, stdout, stderr)
	}

	versions, err := a.ListVersions(context.Background(), proj, legacy, false)
	if err != nil || versions.Degraded || len(versions.Candidates) < 2 {
		t.Errorf("ListVersions(%q) = %+v, %v; want the fixture's tags", legacy, versions, err)
	}

	// A new project that never recorded the legacy spelling gets the
	// ambiguity error instead.
	if _, stderr, code := runGskillWithApp(t, a, newProject(t), "--no-interactive", "add", legacy, "--agent", "claude"); code != 2 {
		t.Errorf("fresh add of %q: exit %d, want 2\n%s", legacy, code, stderr)
	}
}

// TestLegacyLock_SSHSourceRecordedAsHTTPS: before spec 035, an ssh:// source
// was recorded with an HTTPS clone address. Resolving it again keeps the
// commit and content and records the SSH address the user wrote.
func TestLegacyLock_SSHSourceRecordedAsHTTPS(t *testing.T) { //nolint:paralleltest // sets GIT_CONFIG_*
	const declared = "ssh://git@github.com/acme/skills.git"
	fixture := providerFixture(t)
	rewriteRemotes(t, fixture, declared, "https://github.com/acme/skills.git")
	a := noAPIApp(t)
	proj := newProject(t)
	runOK(t, a, proj, "add", declared, "--skill", "demo", "--version", "^1.0.0", "--agent", "claude")

	lockPath := filepath.Join(proj, "skills-lock.json")
	data := readFile(t, lockPath)
	legacy := bytes.ReplaceAll(data, []byte(`"sourceUrl": "`+declared+`"`), []byte(`"sourceUrl": "https://github.com/acme/skills.git"`))
	if bytes.Equal(legacy, data) {
		t.Fatalf("could not plant the legacy HTTPS clone address:\n%s", data)
	}
	if err := os.WriteFile(lockPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	before := lockEntryOf(t, proj, "demo")

	runOK(t, a, proj, "install", "--frozen-lockfile")
	if got := readFile(t, lockPath); !bytes.Equal(got, legacy) {
		t.Errorf("frozen install rewrote the legacy lockfile")
	}
	runOK(t, a, proj, "--json", "upgrade", "demo", "--to", "1.1.0")
	after := lockEntryOf(t, proj, "demo")
	if after.Gskill.Commit != before.Gskill.Commit || after.Gskill.ContentHash != before.Gskill.ContentHash {
		t.Errorf("re-resolving moved the entry: %+v -> %+v", before.Gskill, after.Gskill)
	}
	if after.Gskill.SourceURL != declared {
		t.Errorf("sourceUrl = %q after re-resolving, want the declared %q", after.Gskill.SourceURL, declared)
	}
}
