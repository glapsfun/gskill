package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// TestOpenProjectLockPath: skills-lock.json is the canonical project lock file
// (spec 012 FR-007).
func TestOpenProjectLockPath(t *testing.T) {
	t.Parallel()
	p := openProject("/some/root")
	if got, want := p.lockPath, filepath.Join("/some/root", skillslock.FileName); got != want {
		t.Errorf("lockPath = %q, want %q", got, want)
	}
}

// foreignLock is a shared lock with data gskill does not own: one entry
// installed by gskill (gskill block) and one external-only entry, plus unknown
// fields at both levels.
const foreignLock = `{
  "version": 1,
  "otherToolTop": {"keep": true},
  "skills": {
    "external-only": {
      "source": "acme/skills",
      "sourceType": "github",
      "skillPath": "skills/external-only/SKILL.md",
      "computedHash": "1111111111111111111111111111111111111111111111111111111111111111",
      "otherTool": {"pin": "v1"}
    },
    "managed": {
      "source": "acme/skills",
      "sourceType": "github",
      "skillPath": "skills/managed/SKILL.md",
      "computedHash": "2222222222222222222222222222222222222222222222222222222222222222",
      "gskill": {
        "sourceUrl": "https://github.com/acme/skills.git",
        "commit": "abc123",
        "agents": ["claude"],
        "installMode": "symlink",
        "scope": "project",
        "storeHash": "sha256:3333333333333333333333333333333333333333333333333333333333333333"
      }
    }
  }
}
`

// TestLoadOrNewLockBridgesManagedEntries: only entries carrying a gskill block
// surface in the legacy in-memory view; external-only entries stay invisible
// to legacy code paths (and untouched on disk).
func TestLoadOrNewLockBridgesManagedEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, skillslock.FileName)
	if err := os.WriteFile(path, []byte(foreignLock), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := loadOrNewLock(path)
	if err != nil {
		t.Fatalf("loadOrNewLock: %v", err)
	}
	if len(lf.Skills) != 1 {
		t.Fatalf("legacy view has %d skills, want 1 (managed only): %v", len(lf.Skills), lf.Skills)
	}
	ls, ok := lf.Skills["managed"]
	if !ok {
		t.Fatal("managed entry missing from legacy view")
	}
	if ls.Resolved.ContentHash != "sha256:3333333333333333333333333333333333333333333333333333333333333333" {
		t.Errorf("ContentHash = %q", ls.Resolved.ContentHash)
	}
	if len(ls.Installation.Agents) != 1 || ls.Installation.Agents[0] != "claude" {
		t.Errorf("Agents = %v", ls.Installation.Agents)
	}
}

// TestSaveLockPreservesForeignData: writing the legacy view back must keep
// unknown fields, external-only entries, and the recorded computedHash.
func TestSaveLockPreservesForeignData(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, skillslock.FileName)
	if err := os.WriteFile(path, []byte(foreignLock), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := loadOrNewLock(path)
	if err != nil {
		t.Fatalf("loadOrNewLock: %v", err)
	}
	// Mutate the managed entry (as update would) and save.
	ls := lf.Skills["managed"]
	ls.Resolved.Commit = "def456"
	lf.Skills["managed"] = ls
	if err := saveLock(path, lf); err != nil {
		t.Fatalf("saveLock: %v", err)
	}
	out, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		`"otherToolTop"`,
		`"external-only"`,
		`"otherTool"`,
		`"computedHash": "2222222222222222222222222222222222222222222222222222222222222222"`,
		`"def456"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("saved lock lost %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"abc123"`) {
		t.Errorf("stale commit survived:\n%s", s)
	}
}

// TestSaveLockRemovesDroppedManagedEntries: a managed entry absent from the
// legacy view was removed by gskill; external-only entries are never dropped.
func TestSaveLockRemovesDroppedManagedEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, skillslock.FileName)
	if err := os.WriteFile(path, []byte(foreignLock), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := loadOrNewLock(path)
	if err != nil {
		t.Fatalf("loadOrNewLock: %v", err)
	}
	delete(lf.Skills, "managed")
	if err := saveLock(path, lf); err != nil {
		t.Fatalf("saveLock: %v", err)
	}
	out, _ := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	s := string(out)
	if strings.Contains(s, `"managed"`) {
		t.Errorf("removed managed entry still present:\n%s", s)
	}
	if !strings.Contains(s, `"external-only"`) {
		t.Errorf("external-only entry lost:\n%s", s)
	}
}

// TestSaveLockFreshProject: saving into an empty project creates a valid
// shared-format lock.
func TestSaveLockFreshProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, skillslock.FileName)
	lf := lockfile.New()
	lf.Skills["fresh"] = lockfile.LockedSkill{
		Source:   lockfile.Source{Type: "github", Original: "acme/skills", Owner: "acme", Repo: "skills", Path: "skills/fresh"},
		Resolved: lockfile.Resolved{ContentHash: "sha256:aaaa"},
	}
	if err := saveLock(path, lf); err != nil {
		t.Fatalf("saveLock: %v", err)
	}
	l, err := skillslock.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e, ok := l.Entry("fresh")
	if !ok {
		t.Fatal("fresh entry missing")
	}
	if e.Source != "acme/skills" || e.SourceType != "github" || e.SkillPath != "skills/fresh/SKILL.md" {
		t.Errorf("core fields = %+v", e)
	}
	if e.Ext == nil || e.Ext.StoreHash != "sha256:aaaa" {
		t.Errorf("Ext = %+v", e.Ext)
	}
}
