package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/lockfile"
)

// seedStore imports real content into p's store and returns the content hash and
// stored path, so hash verification passes until the content is tampered with.
func seedStore(t *testing.T, p *project) (string, string) {
	t.Helper()
	content := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(content, 0o750); err != nil {
		t.Fatalf("mkdir content: %v", err)
	}
	if err := os.WriteFile(filepath.Join(content, "SKILL.md"), []byte("# demo\n"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	hashes, err := integrity.HashDir(content)
	if err != nil {
		t.Fatalf("hash content: %v", err)
	}
	storePath, err := p.store.Put(hashes.ContentHash, content)
	if err != nil {
		t.Fatalf("store put: %v", err)
	}
	return hashes.ContentHash, storePath
}

// lockWith builds a single-skill lockfile for the demo skill targeting claude.
func lockWith(name, hash string) *lockfile.Lockfile {
	lf := lockfile.New()
	lf.Skills[name] = lockfile.LockedSkill{
		Resolved: lockfile.Resolved{ContentHash: hash},
		Installation: lockfile.Installation{
			Scope:      "project",
			Agents:     []string{"claude"},
			ActivePath: active.Rel(name),
			Targets:    map[string]string{"claude": filepath.Join(".claude", "skills", name)},
			Modes:      map[string]string{"claude": "symlink"},
		},
	}
	return lf
}

// linkAgent symlinks the claude target to the active entry.
func linkAgent(t *testing.T, root, name string) {
	t.Helper()
	dest := filepath.Join(root, ".claude", "skills", name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	abs, _ := filepath.Abs(active.Path(root, name))
	if err := os.Symlink(abs, dest); err != nil {
		t.Fatalf("symlink agent: %v", err)
	}
}

func newHealthApp() *App {
	return New(Options{Agents: agent.NewDefaultRegistry()})
}

func TestEvaluateHealth_HealthyChain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	linkAgent(t, root, "demo")

	got, err := a.evaluateHealth(p, lockWith("demo", hash), true)
	if err != nil {
		t.Fatalf("evaluateHealth: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1", len(got))
	}
	h := got[0]
	if !h.Healthy() {
		t.Errorf("expected healthy, got faults: %v", h.Faults())
	}
	if h.Agents["claude"] != TargetOKSymlink {
		t.Errorf("claude target = %q, want ok-symlink", h.Agents["claude"])
	}
	if h.ActiveState != active.HealthOK {
		t.Errorf("active = %q, want ok", h.ActiveState)
	}
}

func TestEvaluateHealth_MissingTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	// No agent target created.

	got, err := a.evaluateHealth(p, lockWith("demo", hash), false)
	if err != nil {
		t.Fatalf("evaluateHealth: %v", err)
	}
	if got[0].Agents["claude"] != TargetMissing {
		t.Errorf("claude target = %q, want missing", got[0].Agents["claude"])
	}
	if got[0].Healthy() {
		t.Error("expected unhealthy with a missing target")
	}
}

func TestEvaluateHealth_BrokenLinkAndCorruptStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	linkAgent(t, root, "demo")

	// Corrupt the store content so its hash no longer matches the lock.
	if err := os.WriteFile(filepath.Join(storePath, "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	got, err := a.evaluateHealth(p, lockWith("demo", hash), true)
	if err != nil {
		t.Fatalf("evaluateHealth: %v", err)
	}
	if got[0].StoreHashOK {
		t.Error("expected store hash mismatch")
	}
	if !got[0].IntegrityFault() {
		t.Error("expected an integrity fault")
	}
}

func TestEvaluateHealth_ModeMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	// Recorded mode is symlink, but place a real directory (a copy) instead.
	dest := filepath.Join(root, ".claude", "skills", "demo")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatalf("mkdir copy target: %v", err)
	}

	got, err := a.evaluateHealth(p, lockWith("demo", hash), false)
	if err != nil {
		t.Fatalf("evaluateHealth: %v", err)
	}
	if got[0].Agents["claude"] != TargetModeMismatch {
		t.Errorf("claude target = %q, want mode-mismatch", got[0].Agents["claude"])
	}
}

func TestEvaluateHealth_LegacyDirectStoreLink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, p.store.Root()); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	// Legacy: agent target points directly into the store, not the active entry.
	dest := filepath.Join(root, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	abs, _ := filepath.Abs(storePath)
	if err := os.Symlink(abs, dest); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := a.evaluateHealth(p, lockWith("demo", hash), false)
	if err != nil {
		t.Fatalf("evaluateHealth: %v", err)
	}
	if got[0].Agents["claude"] != TargetLegacyStore {
		t.Errorf("claude target = %q, want legacy-store", got[0].Agents["claude"])
	}
}
