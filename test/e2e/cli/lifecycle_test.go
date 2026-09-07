package cli_test

import (
	"testing"
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
