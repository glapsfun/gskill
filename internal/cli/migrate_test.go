package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// legacyLockProject builds an inited project with a legacy gskill.lock.
func legacyLockProject(t *testing.T) string {
	t.Helper()
	dir := initedProject(t)
	lock := `{
  "lockfile_version": 1,
  "skills": {
    "demo": {
      "source": {"type": "github", "original": "acme/skills", "owner": "acme", "repo": "skills", "path": "skills/demo"},
      "requested": {},
      "resolved": {"ref_kind": "branch", "branch": "main", "commit": "cafe1234", "content_hash": "sha256:aaaa", "mutable_ref": true},
      "metadata": {"name": "demo", "description": "Demo"},
      "requires": {"skills": [], "commands": [], "environment": [], "mcp": []},
      "installation": {"scope": "project", "mode": "symlink", "agents": ["claude"], "targets": {}},
      "provenance": {"trust": "checksum-ok"}
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "gskill.lock"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestMigrateLockfileCommand (T037): the explicit migration command converts,
// backs up, and retires the legacy lock.
func TestMigrateLockfileCommand(t *testing.T) {
	t.Parallel()
	dir := legacyLockProject(t)
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "migrate", "lockfile")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "Migrated 1 skill(s)") {
		t.Errorf("stdout %q should report the migration", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, skillslock.FileName)); err != nil {
		t.Errorf("skills-lock.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill.lock")); err == nil {
		t.Error("gskill.lock still present")
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill.lock.backup")); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}

// TestMigrateLockfileCommand_NoOp: an already-migrated project reports so.
func TestMigrateLockfileCommand_NoOp(t *testing.T) {
	t.Parallel()
	dir := initedProject(t)
	if err := os.WriteFile(filepath.Join(dir, skillslock.FileName),
		[]byte(`{"version": 1, "skills": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "migrate", "lockfile")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "Already migrated") {
		t.Errorf("stdout %q should report the no-op", stdout)
	}
}
