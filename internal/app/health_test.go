package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/integrity"
)

// seedStore imports real content into p's store and returns the content hash and
// stored path, so hash verification passes until the content is tampered with.
func seedStore(t *testing.T, _ *project) (string, string) {
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
	return hashes.ContentHash, content
}

// lockWith builds a single-skill lockfile for the demo skill targeting claude.
func lockWith(name, hash string) *skillslock.State {
	lf := skillslock.NewState()
	lf.Skills[name] = skillslock.Record{
		Resolved: skillslock.Resolved{ContentHash: hash},
		Installation: skillslock.Installation{
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
	if _, err := active.EnsureActive(root, "demo", storePath, active.EnsureOptions{ExpectedHash: hash}); err != nil {
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
	if _, err := active.EnsureActive(root, "demo", storePath, active.EnsureOptions{ExpectedHash: hash}); err != nil {
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

func TestEvaluateHealth_DriftedCommittedContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	linkAgent(t, root, "demo")

	// Hand-edit the committed content so it no longer matches the lock
	// (spec 022 FR-008: drift, reported never repaired).
	if err := os.WriteFile(filepath.Join(active.Path(root, "demo"), "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	got, err := a.evaluateHealth(p, lockWith("demo", hash), true)
	if err != nil {
		t.Fatalf("evaluateHealth: %v", err)
	}
	if got[0].ActiveState != active.HealthDrifted {
		t.Errorf("active state = %q, want drifted", got[0].ActiveState)
	}
	if got[0].Healthy() {
		t.Error("expected unhealthy on drifted committed content")
	}
	faults := got[0].Faults()
	if len(faults) == 0 || !strings.Contains(faults[0], "no longer matches skills-lock.json") {
		t.Errorf("faults = %v, want the drift wording", faults)
	}
}

func TestEvaluateHealth_ModeMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	a := newHealthApp()

	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, active.EnsureOptions{ExpectedHash: hash}); err != nil {
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

	hash, content := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", content, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	// Legacy: agent target points directly into a pre-022 store root, not the
	// active entry.
	legacyObj := filepath.Join(root, ".gskill", "store", "sha256", "x", "content")
	if err := os.MkdirAll(legacyObj, 0o750); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	abs, _ := filepath.Abs(legacyObj)
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

// sharedTestAgent is a shared-location agent (spec 027): its project skill dir
// is the active store itself.
type sharedTestAgent struct{ id string }

func (s sharedTestAgent) ID() string                                       { return s.id }
func (s sharedTestAgent) DisplayName() string                              { return s.id }
func (sharedTestAgent) Detect(context.Context, string) (bool, error)       { return false, nil }
func (sharedTestAgent) ProjectSkillDir(root string) string                 { return active.Dir(root) }
func (s sharedTestAgent) GlobalSkillDir(home string) string                { return filepath.Join(home, s.id) }
func (sharedTestAgent) SupportsSymlinks() bool                             { return true }
func (sharedTestAgent) ValidateInstallation(context.Context, string) error { return nil }

func newSharedHealthApp() *App {
	reg := agent.NewRegistry()
	_ = reg.Register(agent.NewClaudeCode())
	_ = reg.Register(sharedTestAgent{id: "sharedfake"})
	_ = reg.Register(sharedTestAgent{id: "sharedfake2"})
	return New(Options{Agents: reg})
}

// lockWithShared builds a single-skill lockfile whose targets are all shared.
func lockWithShared(name, hash string, ids ...string) *skillslock.State {
	targets := make(map[string]string, len(ids))
	modes := make(map[string]string, len(ids))
	for _, id := range ids {
		targets[id] = active.Rel(name)
		modes[id] = "shared"
	}
	lf := skillslock.NewState()
	lf.Skills[name] = skillslock.Record{
		Resolved: skillslock.Resolved{ContentHash: hash},
		Installation: skillslock.Installation{
			Scope:      "project",
			Agents:     ids,
			ActivePath: active.Rel(name),
			Targets:    targets,
			Modes:      modes,
		},
	}
	return lf
}

// The arrange* helpers build each active-entry state on disk for "demo".
func arrangeActiveOK(t *testing.T, root, storePath, hash string) {
	t.Helper()
	if _, err := active.EnsureActive(root, "demo", storePath, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
}

func arrangeActiveDrifted(t *testing.T, root, storePath, hash string) {
	t.Helper()
	arrangeActiveOK(t, root, storePath, hash)
	if err := os.WriteFile(filepath.Join(active.Path(root, "demo"), "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func arrangeActiveMissing(*testing.T, string, string, string) {}

func arrangeActiveForeign(t *testing.T, root, _, _ string) {
	t.Helper()
	if err := os.MkdirAll(active.Dir(root), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active.Path(root, "demo"), []byte("not a skill"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func arrangeActiveLegacy(t *testing.T, root, _, _ string) {
	t.Helper()
	legacyObj := filepath.Join(root, ".gskill", "store", "sha256", "x", "content")
	if err := os.MkdirAll(legacyObj, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(active.Dir(root), 0o750); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(legacyObj)
	if err := os.Symlink(abs, active.Path(root, "demo")); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateHealth_SharedTargetMirrorsActiveEntry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(t *testing.T, root, storePath, hash string)
		active  active.Health
		want    TargetState
		healthy bool
	}{
		{name: "ok", arrange: arrangeActiveOK, active: active.HealthOK, want: TargetOKShared, healthy: true},
		{name: "drifted", arrange: arrangeActiveDrifted, active: active.HealthDrifted, want: TargetCorrupt},
		{name: "missing", arrange: arrangeActiveMissing, active: active.HealthMissing, want: TargetMissing},
		{name: "foreign", arrange: arrangeActiveForeign, active: active.HealthForeign, want: TargetForeign},
		{name: "legacy", arrange: arrangeActiveLegacy, active: active.HealthLegacy, want: TargetLegacyStore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			p := openProject(root)
			hash, storePath := seedStore(t, p)
			tc.arrange(t, root, storePath, hash)

			got, err := newSharedHealthApp().evaluateHealth(p, lockWithShared("demo", hash, "sharedfake"), true)
			if err != nil {
				t.Fatalf("evaluateHealth: %v", err)
			}
			h := got[0]
			if h.ActiveState != tc.active {
				t.Fatalf("active state = %q, want %q (fixture is wrong)", h.ActiveState, tc.active)
			}
			if h.Agents["sharedfake"] != tc.want {
				t.Errorf("shared target = %q, want %q", h.Agents["sharedfake"], tc.want)
			}
			if h.Healthy() != tc.healthy {
				t.Errorf("Healthy() = %v, want %v (faults %v)", h.Healthy(), tc.healthy, h.Faults())
			}
		})
	}
}

func TestSkillHealth_FaultsReportDriftOnceForSharedTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	hash, storePath := seedStore(t, p)
	if _, err := active.EnsureActive(root, "demo", storePath, active.EnsureOptions{ExpectedHash: hash}); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(active.Path(root, "demo"), "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := newSharedHealthApp().evaluateHealth(p, lockWithShared("demo", hash, "sharedfake", "sharedfake2"), true)
	if err != nil {
		t.Fatalf("evaluateHealth: %v", err)
	}
	faults := got[0].Faults()
	if len(faults) != 1 || !strings.Contains(faults[0], "no longer matches skills-lock.json") {
		t.Errorf("faults = %v, want exactly the one drift line", faults)
	}
}

func TestSkillHealth_SharedDriftIsNotAnIntegrityFault(t *testing.T) {
	t.Parallel()
	h := SkillHealth{
		ActiveState: active.HealthDrifted,
		Agents:      map[string]TargetState{"sharedfake": TargetCorrupt},
		Shared:      map[string]bool{"sharedfake": true},
	}
	if h.IntegrityFault() {
		t.Error("drifted committed content behind a shared target reported as an integrity fault (exit 6), want drift (exit 7)")
	}
	h.Agents["claude"] = TargetCorrupt
	if !h.IntegrityFault() {
		t.Error("a corrupt non-shared copy must still be an integrity fault")
	}
}

func TestEvaluateHealth_MalformedSharedRecord(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*skillslock.Record)
		want   TargetState
		shared bool
	}{
		{name: "well formed", mutate: func(*skillslock.Record) {}, want: TargetOKShared, shared: true},
		{
			name:   "target is not the active entry",
			mutate: func(r *skillslock.Record) { r.Installation.Targets["sharedfake"] = ".agents/skills/other" },
			want:   TargetForeign,
		},
		{
			name:   "mode is not shared",
			mutate: func(r *skillslock.Record) { r.Installation.Modes["sharedfake"] = "copy" },
			want:   TargetModeMismatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			p := openProject(root)
			hash, storePath := seedStore(t, p)
			arrangeActiveOK(t, root, storePath, hash)
			lf := lockWithShared("demo", hash, "sharedfake")
			rec := lf.Skills["demo"]
			tc.mutate(&rec)
			lf.Skills["demo"] = rec

			got, err := newSharedHealthApp().evaluateHealth(p, lf, true)
			if err != nil {
				t.Fatalf("evaluateHealth: %v", err)
			}
			if got[0].Agents["sharedfake"] != tc.want {
				t.Errorf("shared target = %q, want %q", got[0].Agents["sharedfake"], tc.want)
			}
			if got[0].Shared["sharedfake"] != tc.shared {
				t.Errorf("Shared = %v, want %v", got[0].Shared["sharedfake"], tc.shared)
			}
		})
	}
}
