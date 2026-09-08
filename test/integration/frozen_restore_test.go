package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/testutil"
)

func TestFrozenRestore_CleanCheckoutMatchesLockAndIsByteIdentical(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)

	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))
	installedBefore := readFile(t, filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md"))

	// Simulate a clean checkout: drop the state dir and installed content.
	if err := os.RemoveAll(filepath.Join(proj, ".gskill")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(proj, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile"); code != 0 {
		t.Fatalf("frozen restore exit: %s", stderr)
	}

	// Lock is not modified by a frozen restore (SC-002).
	if lockAfter := readFile(t, filepath.Join(proj, "skills-lock.json")); !bytes.Equal(lockBefore, lockAfter) {
		t.Errorf("frozen restore modified the lockfile")
	}
	// Installed content matches the original byte-for-byte (SC-001).
	installedAfter := readFile(t, filepath.Join(proj, ".claude", "skills", "demo", "SKILL.md"))
	if !bytes.Equal(installedBefore, installedAfter) {
		t.Errorf("restored content differs from original")
	}
}

// TestFrozen_EditedVersionExits4BeforeNetwork is spec 024 R8: a frozen
// install compares every declaration to the lock before resolving anything,
// and a disagreement exits 4 with no git call and nothing written.
func TestFrozen_EditedVersionExits4BeforeNetwork(t *testing.T) {
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
	initProjectWithApp(t, a, proj)
	if _, stderr, code := runGskillWithApp(t, a, proj, "add", repo, "--version", "^1.0.0"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	setManifestKey(t, proj, "demo", "version", "1.1.0")
	manifest, lock := readFile(t, manifestPath(proj)), readFile(t, filepath.Join(proj, "skills-lock.json"))

	before := counting.ResolutionCalls()
	_, stderr, code := runGskillWithApp(t, a, proj, "install", "--frozen-lockfile")
	if code != 4 {
		t.Errorf("exit = %d, want 4 (lock mismatch): %s", code, stderr)
	}
	for _, want := range []string{"demo", "version"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q: %s", want, stderr)
		}
	}
	if got := counting.ResolutionCalls(); got != before {
		t.Errorf("frozen pre-flight made %d git calls", got-before)
	}
	if !bytes.Equal(manifest, readFile(t, manifestPath(proj))) || !bytes.Equal(lock, readFile(t, filepath.Join(proj, "skills-lock.json"))) {
		t.Error("frozen run wrote a file")
	}
}

// TestFrozen_ManifestAgentNarrowingRefused: --frozen-lockfile promises to
// restore exactly what the lock records and never modify it. A manifest that
// narrows the agent list is a declaration change, so the pre-flight must fail
// closed. Before the fix the narrowing slipped past the pre-flight, the run
// deleted the dropped agent's target, and frozen mode then suppressed the lock
// write — leaving a lock that still declared a target no longer on disk.
func TestFrozen_ManifestAgentNarrowingRefused(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo, "--agent", "claude,codex"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	codexTarget := filepath.Join(proj, ".codex", "skills", "demo")
	if _, err := os.Stat(codexTarget); err != nil {
		t.Fatalf("codex target missing after add: %v", err)
	}

	data := readFile(t, manifestPath(proj))
	narrowed := bytes.Replace(data, []byte(`agents = ["claude", "codex"]`), []byte(`agents = ["claude"]`), 1)
	if bytes.Equal(data, narrowed) {
		t.Fatalf("could not narrow the agent list in:\n%s", data)
	}
	if err := os.WriteFile(manifestPath(proj), narrowed, 0o600); err != nil {
		t.Fatal(err)
	}
	lockBefore := readFile(t, filepath.Join(proj, "skills-lock.json"))

	_, stderr, code := runGskill(t, proj, "install", "--frozen-lockfile")
	if code != 4 {
		t.Errorf("frozen install with narrowed agents exit = %d, want 4 (lock mismatch); stderr %s", code, stderr)
	}
	if !strings.Contains(stderr, "agents") {
		t.Errorf("refusal does not name the changed key: %s", stderr)
	}
	if _, err := os.Stat(codexTarget); err != nil {
		t.Errorf("frozen install deleted the codex target it was told not to touch: %v", err)
	}
	if !bytes.Equal(lockBefore, readFile(t, filepath.Join(proj, "skills-lock.json"))) {
		t.Error("frozen install rewrote the lockfile")
	}
}
