package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/store"
)

// Phase-API integration tests for spec 011 (contracts/app-phases.md):
// DiscoverSource → PlanInstall → ExecutePlan, with PlanInstall proven
// write-free and the composition proven equivalent to App.Add.

func onboardApp() *app.App {
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// treeDigest hashes every file under root (path + content), skipping the
// volatile .gskill lock directory, so tests can assert "nothing changed".
func treeDigest(t *testing.T, root string) string {
	t.Helper()

	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
		if err != nil {
			return err
		}
		h.Write([]byte(rel + "\n"))
		h.Write(data)
		return nil
	})
	if err != nil {
		t.Fatalf("treeDigest: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func discover(t *testing.T, a *app.App, root, src string) app.DiscoverResult {
	t.Helper()

	disc, err := a.DiscoverSource(context.Background(), app.DiscoverRequest{Root: root, Source: src})
	if err != nil {
		t.Fatalf("DiscoverSource: %v", err)
	}
	return disc
}

func selectByID(t *testing.T, disc app.DiscoverResult, ids ...string) []discovery.DiscoveredSkill {
	t.Helper()

	var out []discovery.DiscoveredSkill
	for _, id := range ids {
		found := false
		for _, s := range disc.Skills {
			if s.ID == id {
				out = append(out, s)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("skill %q not in discovery result", id)
		}
	}
	return out
}

func TestDiscoverSource_ReturnsCatalogWithoutManifest(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha", "skills/beta", "skills/broken")
	root := t.TempDir() // no manifest: discovery must still work (wizard on fresh dir)

	disc := discover(t, onboardApp(), root, src)
	if len(disc.Skills) != 3 {
		t.Fatalf("got %d skills, want 3", len(disc.Skills))
	}
	byID := map[string]discovery.DiscoveredSkill{}
	for _, s := range disc.Skills {
		byID[s.ID] = s
	}
	if !byID["alpha"].Valid || !byID["beta"].Valid {
		t.Error("alpha/beta should be valid")
	}
	if byID["broken"].Valid {
		t.Error("broken should be invalid (missing description)")
	}
}

func TestPlanInstall_WritesNothingAndPlansActions(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha", "skills/beta")
	root := projectWithAgent(t)
	a := onboardApp()
	disc := discover(t, a, root, src)

	before := treeDigest(t, root)
	plan, err := a.PlanInstall(context.Background(), app.PlanRequest{
		Root:     root,
		Source:   src,
		Discover: disc,
		Selected: selectByID(t, disc, "alpha", "beta"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if after := treeDigest(t, root); after != before {
		t.Error("PlanInstall modified the project tree; it must be read-only")
	}

	if plan.InitProject {
		t.Error("InitProject = true for an inited project")
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", plan.Conflicts)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("got %d actions, want 2 (2 skills × 1 agent)", len(plan.Actions))
	}
	var dests []string
	for _, act := range plan.Actions {
		if act.AgentID != "claude" {
			t.Errorf("action agent = %q, want claude", act.AgentID)
		}
		dests = append(dests, act.Destination)
	}
	sort.Strings(dests)
	wantAlpha := filepath.Join(root, ".claude", "skills", "alpha")
	if dests[0] != wantAlpha {
		t.Errorf("destination = %q, want %q", dests[0], wantAlpha)
	}
}

func TestPlanInstall_FreshDirPlansInit(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := t.TempDir() // no gskill.toml
	a := onboardApp()
	disc := discover(t, a, root, src)

	plan, err := a.PlanInstall(context.Background(), app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if !plan.InitProject {
		t.Error("InitProject = false on a fresh dir, want true (FR-023)")
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.toml")); err == nil {
		t.Error("PlanInstall scaffolded the manifest; init must happen only on ExecutePlan")
	}
}

func TestPlanInstall_DetectsConflicts(t *testing.T) {
	t.Parallel()

	srcA := sourceTree(t, "skills/alpha")
	srcB := sourceTree(t, "skills/alpha") // same name, different source
	root := projectWithAgent(t)
	a := onboardApp()

	if _, err := a.Add(context.Background(), app.AddRequest{Root: root, Source: srcA}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}

	// Cross-source name collision.
	discB := discover(t, a, root, srcB)
	plan, err := a.PlanInstall(context.Background(), app.PlanRequest{
		Root: root, Source: srcB, Discover: discB,
		Selected: selectByID(t, discB, "alpha"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != app.ConflictCrossSource {
		t.Errorf("Conflicts = %+v, want one %s", plan.Conflicts, app.ConflictCrossSource)
	}

	// Re-add with nothing new.
	discA := discover(t, a, root, srcA)
	plan, err = a.PlanInstall(context.Background(), app.PlanRequest{
		Root: root, Source: srcA, Discover: discA,
		Selected: selectByID(t, discA, "alpha"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != app.ConflictNoopReadd {
		t.Errorf("Conflicts = %+v, want one %s", plan.Conflicts, app.ConflictNoopReadd)
	}
}

func TestExecutePlan_RefusesConflictedPlan(t *testing.T) {
	t.Parallel()

	srcA := sourceTree(t, "skills/alpha")
	srcB := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()

	if _, err := a.Add(context.Background(), app.AddRequest{Root: root, Source: srcA}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	discB := discover(t, a, root, srcB)
	plan, err := a.PlanInstall(context.Background(), app.PlanRequest{
		Root: root, Source: srcB, Discover: discB,
		Selected: selectByID(t, discB, "alpha"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}

	_, err = a.ExecutePlan(context.Background(), plan, nil)
	if err == nil {
		t.Fatal("ExecutePlan accepted a conflicted plan")
	}
	if !errors.Is(err, errs.ErrInvalidManifest) {
		t.Errorf("err = %v, want errs.ErrInvalidManifest (same category as non-guided add)", err)
	}
}

func TestExecutePlan_InitsFreshProjectAndInstalls(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	a := onboardApp()
	disc := discover(t, a, root, src)
	plan, err := a.PlanInstall(context.Background(), app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}

	var events []app.ProgressEvent
	res, err := a.ExecutePlan(context.Background(), plan, func(e app.ProgressEvent) { events = append(events, e) })
	if err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	if len(res.Installed) != 1 || res.Installed[0].Name != "alpha" {
		t.Fatalf("Installed = %+v, want alpha", res.Installed)
	}
	for _, f := range []string{"gskill.toml", "gskill.lock"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("%s missing after ExecutePlan on a fresh dir: %v", f, err)
		}
	}
	if len(events) == 0 {
		t.Error("no progress events emitted")
	}
	sawAlpha := false
	for _, e := range events {
		if e.Skill == "alpha" {
			sawAlpha = true
		}
	}
	if !sawAlpha {
		t.Errorf("events = %+v, want at least one for skill alpha", events)
	}
}

// TestAddParity_PhasesProduceIdenticalLockfile is the SC-004/constitution-I
// parity gate: the linear phase composition must produce the same lockfile as
// App.Add for identical inputs (provenance timestamps normalized).
func TestAddParity_PhasesProduceIdenticalLockfile(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha", "skills/beta")

	rootAdd := projectWithAgent(t)
	rootPhases := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	if _, err := a.Add(ctx, app.AddRequest{Root: rootAdd, Source: src, All: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	disc := discover(t, a, rootPhases, src)
	plan, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: rootPhases, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha", "beta"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if _, err := a.ExecutePlan(ctx, plan, nil); err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}

	lockAdd := normalizedLock(t, filepath.Join(rootAdd, "gskill.lock"))
	lockPhases := normalizedLock(t, filepath.Join(rootPhases, "gskill.lock"))
	if lockAdd != lockPhases {
		t.Errorf("lockfiles differ between Add and phase composition:\nAdd:    %s\nPhases: %s", lockAdd, lockPhases)
	}
}

// normalizedLock loads a lockfile and zeroes volatile provenance timestamps and
// machine-specific local source paths, returning canonical JSON.
func normalizedLock(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	skills, _ := v["skills"].(map[string]any)
	for _, sv := range skills {
		sm, _ := sv.(map[string]any)
		if prov, ok := sm["provenance"].(map[string]any); ok {
			prov["fetched_at"] = ""
			prov["updated_at"] = ""
		}
		// The two projects install from the same temp source; the recorded
		// source is identical. Nothing else is machine-specific here.
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-marshal lock: %v", err)
	}
	return string(out)
}

// ---- SC-006 failure classes (spec 011 T013) ----------------------------------

// assertNoPartialInstall checks that a failed execute left no lockfile, no
// manifest entry, and no activated agent target (FR-020).
func assertNoPartialInstall(t *testing.T, root, skill string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(root, "gskill.lock")); err == nil {
		t.Error("lockfile exists after failed install")
	}
	data, err := os.ReadFile(filepath.Join(root, "gskill.toml")) //nolint:gosec // test-controlled temp path
	if err == nil && len(data) > 0 && contains(string(data), skill) {
		t.Errorf("manifest records %q after failed install", skill)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", skill)); err == nil {
		t.Error("agent target exists after failed install (rollback missed it)")
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && strings.Contains(s, sub)
}

func TestDiscoverSource_UnreachableSourceFailsTyped(t *testing.T) {
	t.Parallel()

	root := projectWithAgent(t)
	_, err := onboardApp().DiscoverSource(context.Background(), app.DiscoverRequest{
		Root:   root,
		Source: filepath.Join(t.TempDir(), "definitely-missing"),
	})
	if err == nil {
		t.Fatal("DiscoverSource succeeded for a nonexistent source")
	}
	assertNoPartialInstall(t, root, "alpha")
}

func TestExecutePlan_ChecksumMismatchFailsClosed(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	a := onboardApp()
	ctx := context.Background()

	// Learn the content hash from a clean install elsewhere.
	seed := projectWithAgent(t)
	seedRes, err := a.Add(ctx, app.AddRequest{Root: seed, Source: src})
	if err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	hash := seedRes.Installed[0].ContentHash

	// Tamper the victim project's store entry for that hash, so stage-and-verify
	// re-hashes different content than the key promises.
	root := projectWithAgent(t)
	storeEntry := store.New(filepath.Join(root, ".gskill", "store")).Path(hash)
	if err := os.MkdirAll(storeEntry, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeEntry, "SKILL.md"), []byte("---\nname: alpha\ndescription: tampered\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	disc := discover(t, a, root, src)
	plan, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha"), AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	_, err = a.ExecutePlan(ctx, plan, nil)
	if err == nil {
		t.Fatal("ExecutePlan succeeded with a tampered store entry")
	}
	if !errors.Is(err, errs.ErrIntegrity) {
		t.Errorf("err = %v, want errs.ErrIntegrity", err)
	}
	assertNoPartialInstall(t, root, "alpha")
}

func TestExecutePlan_CancelledMidInstallRollsBack(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha", "skills/beta")
	root := projectWithAgent(t)
	a := onboardApp()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	disc := discover(t, a, root, src)
	plan, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha", "beta"), AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}

	// Cancel as soon as the first skill records: the second iteration must
	// abort, and the already-activated first skill must be rolled back.
	_, err = a.ExecutePlan(ctx, plan, func(e app.ProgressEvent) {
		if e.Stage == "record" {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("ExecutePlan succeeded despite mid-install cancellation")
	}
	assertNoPartialInstall(t, root, "alpha")
	assertNoPartialInstall(t, root, "beta")
}
