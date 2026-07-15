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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/store"
	"github.com/glapsfun/gskill/internal/testutil"
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
	if !errors.Is(err, errs.ErrInvalidLock) {
		t.Errorf("err = %v, want errs.ErrInvalidLock (same category as non-guided add)", err)
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

	var events []app.InstallProgressEvent
	res, err := a.ExecutePlan(context.Background(), plan, func(e app.InstallProgressEvent) { events = append(events, e) })
	if err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	if len(res.Installed) != 1 || res.Installed[0].Name != skillAlpha {
		t.Fatalf("Installed = %+v, want alpha", res.Installed)
	}
	for _, f := range []string{"skills-lock.json", ".gskill"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("%s missing after ExecutePlan on a fresh dir: %v", f, err)
		}
	}
	if len(events) == 0 {
		t.Error("no progress events emitted")
	}
	sawAlpha := false
	for _, e := range events {
		if e.SkillName == skillAlpha {
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

	lockAdd := normalizedLock(t, filepath.Join(rootAdd, "skills-lock.json"))
	lockPhases := normalizedLock(t, filepath.Join(rootPhases, "skills-lock.json"))
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
		if ext, ok := sm["gskill"].(map[string]any); ok {
			ext["installedAt"] = ""
			ext["updatedAt"] = ""
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

	if _, err := os.Stat(filepath.Join(root, "skills-lock.json")); err == nil {
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
	_, err = a.ExecutePlan(ctx, plan, func(e app.InstallProgressEvent) {
		if e.Status == app.InstallStatusInstalled {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("ExecutePlan succeeded despite mid-install cancellation")
	}
	assertNoPartialInstall(t, root, "alpha")
	assertNoPartialInstall(t, root, "beta")
}

// ---- US2: agent choices and multi-agent plans (spec 011 T021/T022) -------------

func TestAgentChoices_MarksDetectedAndPreselectsDefaults(t *testing.T) {
	t.Parallel()

	root := projectWithAgent(t) // .claude marker only
	choices, err := onboardApp().AgentChoices(context.Background(), root)
	if err != nil {
		t.Fatalf("AgentChoices: %v", err)
	}
	if len(choices) < 3 {
		t.Fatalf("got %d choices, want the full registry", len(choices))
	}
	byID := map[string]app.AgentChoice{}
	for _, c := range choices {
		byID[c.ID] = c
	}
	claude := byID["claude"]
	if !claude.Detected || !claude.Preselected {
		t.Errorf("claude = %+v, want detected and preselected", claude)
	}
	if codex := byID["codex"]; codex.Detected || codex.Preselected {
		t.Errorf("codex = %+v, want neither detected nor preselected", codex)
	}
}

func TestExecutePlan_MultiAgentRecordsBothTargets(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o750); err != nil {
		t.Fatal(err)
	}
	a := onboardApp()
	ctx := context.Background()

	disc := discover(t, a, root, src)
	plan, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha"),
		AgentIDs: []string{"claude", "codex"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("got %d actions, want 2 (1 skill × 2 agents)", len(plan.Actions))
	}

	if _, err := a.ExecutePlan(ctx, plan, nil); err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	for _, dir := range []string{".claude", ".codex"} {
		if _, err := os.Stat(filepath.Join(root, dir, "skills", "alpha")); err != nil {
			t.Errorf("agent target %s/skills/alpha missing: %v", dir, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	for _, id := range []string{"claude", "codex"} {
		if !strings.Contains(string(data), `"`+id+`"`) {
			t.Errorf("lockfile does not record agent %q", id)
		}
	}
}

// ---- US3: version listing and pinning (spec 011 T025/T028) ---------------------

// gitSource builds a local git repo with one skill and two tagged versions,
// returning the repo path and each tag's commit.
func gitSource(t *testing.T) (repo, commitV1, commitV2 string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, "SKILL.md"),
			[]byte("---\nname: demo\ndescription: "+body+"\n---\n# demo "+body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet", "-b", "main")
	write("one")
	run("add", ".")
	run("commit", "--quiet", "-m", "v1")
	run("tag", "v1.0.0")
	commitV1 = run("rev-parse", "HEAD")
	write("two")
	run("add", ".")
	run("commit", "--quiet", "-m", "v2")
	run("tag", "v2.0.0")
	commitV2 = run("rev-parse", "HEAD")
	return repo, commitV1, commitV2
}

func TestListVersions_GitSourceOffersReleasesAndBranches(t *testing.T) {
	t.Parallel()

	repo, _, _ := gitSource(t)
	root := projectWithAgent(t)

	vl, err := onboardApp().ListVersions(context.Background(), root, repo, false)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if vl.Degraded {
		t.Fatalf("Degraded = true for a healthy local git source: %s", vl.DegradedReason)
	}
	var labels []string
	for _, c := range vl.Candidates {
		labels = append(labels, c.Label)
	}
	if len(vl.Candidates) == 0 || vl.Candidates[0].Kind != app.VersionLatest {
		t.Fatalf("first candidate must be latest, got %v", labels)
	}
	joined := strings.Join(labels, " ")
	for _, want := range []string{"v2.0.0", "v1.0.0", "main"} {
		if !strings.Contains(joined, want) {
			t.Errorf("candidates %v missing %q", labels, want)
		}
	}
}

func TestListVersions_OfflineDegradesGracefully(t *testing.T) {
	t.Parallel()

	repo, _, _ := gitSource(t)
	root := projectWithAgent(t)

	vl, err := onboardApp().ListVersions(context.Background(), root, repo, true)
	if err != nil {
		t.Fatalf("ListVersions offline must not fail the flow: %v", err)
	}
	if !vl.Degraded || vl.DegradedReason == "" {
		t.Errorf("offline listing must be marked degraded with a reason: %+v", vl)
	}
	if len(vl.Candidates) == 0 || vl.Candidates[0].Kind != app.VersionLatest {
		t.Errorf("degraded listing must still preselect latest: %+v", vl.Candidates)
	}
}

func TestListVersions_PlainLocalSourceIsLatestOnly(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha") // not a git repo
	root := projectWithAgent(t)

	vl, err := onboardApp().ListVersions(context.Background(), root, src, false)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(vl.Candidates) != 1 || vl.Candidates[0].Kind != app.VersionLatest {
		t.Errorf("plain local source must offer only latest: %+v", vl.Candidates)
	}
}

// TestPlanInstall_ReResolvesChangedVersionPin is the FR-013 correctness gate:
// picking a version in the wizard AFTER discovery must re-pin the plan to that
// version's exact commit, not keep the discovery-time (latest) resolution.
func TestPlanInstall_ReResolvesChangedVersionPin(t *testing.T) {
	t.Parallel()

	repo, commitV1, commitV2 := gitSource(t)
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	disc := discover(t, a, root, repo) // default resolution → v2.0.0
	if disc.Revision.Commit != commitV2 {
		t.Fatalf("discovery resolved %s, want latest %s", disc.Revision.Commit, commitV2)
	}

	plan, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: repo, Ref: "v1.0.0",
		Discover: disc, Selected: disc.Skills[:1], AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if plan.Revision.Commit != commitV1 {
		t.Fatalf("plan.Revision.Commit = %s, want v1.0.0's %s (stale discovery pin, FR-013)", plan.Revision.Commit, commitV1)
	}

	if _, err := a.ExecutePlan(ctx, plan, nil); err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	lock, err := os.ReadFile(filepath.Join(root, "skills-lock.json")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if !strings.Contains(string(lock), commitV1) {
		t.Errorf("lockfile does not pin v1.0.0's commit %s", commitV1)
	}
	if strings.Contains(string(lock), commitV2) {
		t.Errorf("lockfile still references the stale latest commit %s", commitV2)
	}
}

// ---- Review fixes, Phase C: re-pin must re-discover ------------------------------

// gitSourceWithMovedContent builds a git repo where v1.0.0 has an extra file
// and a v2-only skill appears later, so plans pinned to v1 must reflect v1.
func gitSourceWithMovedContent(t *testing.T) (repo string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet", "-b", "main")
	write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: v1\n---\n# alpha v1\n")
	write("skills/alpha/V1ONLY.md", "only in v1\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "v1")
	run("tag", "v1.0.0")

	if err := os.Remove(filepath.Join(repo, "skills", "alpha", "V1ONLY.md")); err != nil {
		t.Fatal(err)
	}
	write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: v2\n---\n# alpha v2\n")
	write("skills/newcomer/SKILL.md", "---\nname: newcomer\ndescription: v2 only\n---\n# newcomer\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "v2")
	run("tag", "v2.0.0")
	return repo
}

func TestPlanInstall_RePinReDiscoversContent(t *testing.T) {
	t.Parallel()

	repo := gitSourceWithMovedContent(t)
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	disc := discover(t, a, root, repo) // latest = v2.0.0
	plan, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: repo, Ref: "v1.0.0",
		Discover: disc, Selected: selectByID(t, disc, "alpha"),
		AgentIDs: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}

	sawV1Only := false
	for _, act := range plan.Actions {
		for _, op := range act.FileOps {
			if filepath.Base(op.Path) == "V1ONLY.md" {
				sawV1Only = true
			}
		}
	}
	if !sawV1Only {
		t.Errorf("plan pinned to v1.0.0 does not list v1's files; the preview describes the wrong version: %+v", plan.Actions)
	}
}

func TestPlanInstall_RePinMissingSkillFailsBeforeApproval(t *testing.T) {
	t.Parallel()

	repo := gitSourceWithMovedContent(t)
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	disc := discover(t, a, root, repo) // latest: has "newcomer"
	_, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: repo, Ref: "v1.0.0",
		Discover: disc, Selected: selectByID(t, disc, "newcomer"),
		AgentIDs: []string{"claude"},
	})
	if err == nil {
		t.Fatal("planning a v2-only skill at v1.0.0 succeeded; it must fail before approval")
	}
	if !strings.Contains(err.Error(), "newcomer") {
		t.Errorf("error does not name the missing skill: %v", err)
	}
}

// ---- Review fixes, Phase D: single agent detection pass --------------------------

// countingAgent wraps an agent adapter and counts Detect calls.
type countingAgent struct {
	agent.Agent
	detects *int32
}

func (c countingAgent) Detect(ctx context.Context, root string) (bool, error) {
	atomic.AddInt32(c.detects, 1)
	return c.Agent.Detect(ctx, root)
}

func countingApp(t *testing.T) (*app.App, *int32) {
	t.Helper()

	var detects int32
	reg := agent.NewRegistry()
	if err := reg.Register(countingAgent{Agent: agent.NewClaudeCode(), detects: &detects}); err != nil {
		t.Fatal(err)
	}
	a := app.New(app.Options{Agents: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return a, &detects
}

func TestAdd_DetectsAgentsOnce(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a, detects := countingApp(t)

	if _, err := a.Add(context.Background(), app.AddRequest{Root: root, Source: src}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := atomic.LoadInt32(detects); got > 1 {
		t.Errorf("agent detection ran %d times during one add, want 1", got)
	}
}

func TestAgentChoices_DetectsOnce(t *testing.T) {
	t.Parallel()

	root := projectWithAgent(t)
	a, detects := countingApp(t)

	choices, err := a.AgentChoices(context.Background(), root)
	if err != nil {
		t.Fatalf("AgentChoices: %v", err)
	}
	if got := atomic.LoadInt32(detects); got > 1 {
		t.Errorf("agent detection ran %d times in AgentChoices, want 1", got)
	}
	if len(choices) != 1 || !choices[0].Detected || !choices[0].Preselected {
		t.Errorf("choices = %+v, want claude detected and preselected", choices)
	}
}

// ---- Review round 2, Phase 3: fast-path gate fidelity -----------------------------

func TestQualifiesLocalAgentAdd_RespectsScopeModeAndPath(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()
	if _, err := a.Add(ctx, app.AddRequest{Root: root, Source: src}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}

	base := app.AddRequest{Root: root, Source: src, Agents: []string{"codex"}}
	if !a.QualifiesLocalAgentAdd(ctx, root, base) {
		t.Fatal("plain agent-add should qualify for the local fast path")
	}

	tests := []struct {
		name   string
		mutate func(*app.AddRequest)
	}{
		{"--global changes placement", func(r *app.AddRequest) { r.Scope = "global" }},
		{"--copy changes mode", func(r *app.AddRequest) { r.Mode = "copy" }},
		{"--path selects different content", func(r *app.AddRequest) { r.Path = "skills/other" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := base
			tt.mutate(&req)
			if a.QualifiesLocalAgentAdd(ctx, root, req) {
				t.Errorf("%s: request must not take the locked-scope relink fast path", tt.name)
			}
		})
	}
}

// ---- Review round 2, Phase 4: plan fidelity ----------------------------------------

// gitSourceWithVendorDup builds a repo where v2 (latest) has skills/tools and
// an excludable vendor/tools duplicate, while v1 has ONLY the vendor copy.
func gitSourceWithVendorDup(t *testing.T) (repo string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, desc string) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\nname: tools\ndescription: "+desc+"\n---\n# tools\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet", "-b", "main")
	write("vendor/tools/SKILL.md", "vendored v1")
	run("add", ".")
	run("commit", "--quiet", "-m", "v1")
	run("tag", "v1.0.0")
	write("skills/tools/SKILL.md", "real v2")
	run("add", ".")
	run("commit", "--quiet", "-m", "v2")
	run("tag", "v2.0.0")
	return repo
}

func TestPlanInstall_RePinKeepsDiscoveryFilters(t *testing.T) {
	t.Parallel()

	repo := gitSourceWithVendorDup(t)
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	// Discover at latest with the vendor copy excluded, exactly as the flags did.
	disc, err := a.DiscoverSource(ctx, app.DiscoverRequest{
		Root: root, Source: repo, Exclude: []string{"vendor/**"},
	})
	if err != nil {
		t.Fatalf("DiscoverSource: %v", err)
	}

	// Pinning v1.0.0 (where only the excluded vendor copy exists) must fail
	// closed, not silently remap the selection onto the excluded skill.
	_, err = a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: repo, Ref: "v1.0.0",
		Discover: disc, Selected: selectByID(t, disc, "tools"),
		AgentIDs: []string{"claude"},
		Exclude:  []string{"vendor/**"},
	})
	if err == nil {
		t.Fatal("re-pin remapped onto the excluded vendor copy instead of failing closed")
	}
	if !strings.Contains(err.Error(), "tools") {
		t.Errorf("error does not name the missing skill: %v", err)
	}
}

func TestPlanInstall_CorruptLockfileFailsClosed(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(root, "skills-lock.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	disc := discover(t, a, root, src)
	_, err := a.PlanInstall(ctx, app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha"), AgentIDs: []string{"claude"},
	})
	if err == nil {
		t.Fatal("PlanInstall silently planned against a corrupt lockfile (drift must be an error)")
	}
}

// ---- Review round 2, Phase 5: AgentChoices error fidelity ---------------------------

func TestAgentChoices_EmptyResolutionFailsActionably(t *testing.T) {
	t.Parallel()

	root := t.TempDir() // no markers, no manifest
	reg := agent.NewRegistry()
	if err := reg.Register(agent.NewCodex()); err != nil { // registry without the default agent
		t.Fatal(err)
	}
	a := app.New(app.Options{Agents: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	_, err := a.AgentChoices(context.Background(), root)
	if err == nil {
		t.Fatal("no-preselection registry returned silently; the agents step would softlock")
	}
	if !strings.Contains(err.Error(), "none detected") {
		t.Errorf("error = %v, want the actionable none-detected wording", err)
	}
}
