package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// US4 (spec 011 T029): file-level plan detail — create/update classification
// and foreign-destination overwrite conflicts, matching what execution will do.

func planFor(t *testing.T, a *app.App, root, src string, force bool, ids ...string) app.InstallPlan {
	t.Helper()

	disc := discover(t, a, root, src)
	plan, err := a.PlanInstall(context.Background(), app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, ids...),
		AgentIDs: []string{"claude"},
		Force:    force,
	})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	return plan
}

func TestPlanInstall_FileOpsCreateForFreshInstall(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	plan := planFor(t, onboardApp(), root, src, false, "alpha")

	if len(plan.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(plan.Actions))
	}
	ops := plan.Actions[0].FileOps
	if len(ops) == 0 {
		t.Fatal("no FileOps planned; the preview must list the files to be written")
	}
	sawSkillMD := false
	for _, op := range ops {
		if op.Op != "create" {
			t.Errorf("op = %+v, want create on a fresh destination", op)
		}
		if filepath.Base(op.Path) == "SKILL.md" {
			sawSkillMD = true
		}
	}
	if !sawSkillMD {
		t.Errorf("FileOps %+v missing SKILL.md", ops)
	}
}

func TestPlanInstall_FileOpsUpdateOnForceReadd(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()
	if _, err := a.Add(context.Background(), app.AddRequest{Root: root, Source: src}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}

	plan := planFor(t, a, root, src, true, "alpha") // --force re-add
	if len(plan.Conflicts) != 0 {
		t.Fatalf("force re-add must not conflict: %+v", plan.Conflicts)
	}
	if len(plan.Actions) != 1 || len(plan.Actions[0].FileOps) == 0 {
		t.Fatalf("actions = %+v, want file ops", plan.Actions)
	}
	for _, op := range plan.Actions[0].FileOps {
		if op.Op != "update" {
			t.Errorf("op = %+v, want update over the existing install", op)
		}
	}
}

func TestPlanInstall_ForeignDestinationIsOverwriteConflict(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	foreign := filepath.Join(root, ".claude", "skills", "alpha")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("# hand-written\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := planFor(t, onboardApp(), root, src, false, "alpha")
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != app.ConflictFileOverwrite {
		t.Fatalf("Conflicts = %+v, want one %s (undeclared destination already occupied)", plan.Conflicts, app.ConflictFileOverwrite)
	}
}

// ---- Review fixes, Phase C -------------------------------------------------------

func TestPlanInstall_ForceOverridesForeignDestination(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	foreign := filepath.Join(root, ".claude", "skills", "alpha")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("# hand-written\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := planFor(t, onboardApp(), root, src, true, "alpha") // --force
	if len(plan.Conflicts) != 0 {
		t.Fatalf("--force did not override the overwrite conflict (FR-016 escape hatch): %+v", plan.Conflicts)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("actions = %+v, want the forced install planned", plan.Actions)
	}
}

func TestPlanInstall_ManagedStoreSymlinkIsNotForeign(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)

	// Simulate a lost lockfile with gskill's own store symlink still active:
	// .claude/skills/alpha -> <root>/.gskill/store/<hash>.
	storeDir := filepath.Join(root, ".gskill", "store", "sha256", "ab", "abcdef")
	if err := os.MkdirAll(storeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(linkParent, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeDir, filepath.Join(linkParent, "alpha")); err != nil {
		t.Fatal(err)
	}

	plan := planFor(t, onboardApp(), root, src, false, "alpha")
	if len(plan.Conflicts) != 0 {
		t.Fatalf("gskill's own store symlink flagged as foreign content: %+v", plan.Conflicts)
	}
}

func TestPlanInstall_GlobalScopeFailsWithoutHome(t *testing.T) {
	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()
	disc := discover(t, a, root, src)

	t.Setenv("HOME", "")
	_, err := a.PlanInstall(context.Background(), app.PlanRequest{
		Root: root, Source: src, Discover: disc,
		Selected: selectByID(t, disc, "alpha"),
		AgentIDs: []string{"claude"},
		Scope:    "global",
	})
	if err == nil {
		t.Fatal("global-scope plan with no resolvable home succeeded; destinations would be garbage")
	}
}

// TestInstallPlan_LinesCoverEveryPlanElement guards the shared renderer both
// the wizard preview and `add --dry-run` consume (FR-015/FR-024).
func TestInstallPlan_LinesCoverEveryPlanElement(t *testing.T) {
	t.Parallel()

	plan := app.InstallPlan{
		Source:      "example/repo",
		AgentIDs:    []string{"claude", "codex"},
		InitProject: true,
		Actions: []app.PlannedAction{
			{
				Skill: "alpha", AgentID: "codex", Destination: "/d/codex/alpha",
				FileOps: []app.PlannedFileOp{{Path: "/d/codex/alpha/SKILL.md", Op: "create"}},
			},
			{Skill: "alpha", AgentID: "claude", Destination: "/d/claude/alpha"},
		},
		Warnings:  []string{"floating branch"},
		Conflicts: []app.PlanConflict{{Skill: "beta", Kind: app.ConflictCrossSource, Detail: "beta collides"}},
	}

	var kinds []string
	var texts []string
	for _, pl := range plan.Lines("v1.2.3") {
		kinds = append(kinds, pl.Kind)
		texts = append(texts, pl.Text)
	}
	joined := strings.Join(texts, "\n")
	for _, want := range []string{
		"Source:  example/repo", "Version: v1.2.3", "Agents:  claude, codex",
		"will be created", "claude:", "codex:",
		"alpha → /d/codex/alpha", "create /d/codex/alpha/SKILL.md",
		"floating branch", "beta collides",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Lines missing %q:\n%s", want, joined)
		}
	}
	// Agent groups render sorted for deterministic output.
	if idx := strings.Index(joined, "claude:"); idx > strings.Index(joined, "codex:") {
		t.Errorf("agent groups not sorted:\n%s", joined)
	}
	wantKinds := map[string]bool{}
	for _, k := range kinds {
		wantKinds[k] = true
	}
	for _, k := range []string{app.PlanLineMeta, app.PlanLineInit, app.PlanLineAgent, app.PlanLineAction, app.PlanLineFileOp, app.PlanLineWarning, app.PlanLineConflict} {
		if !wantKinds[k] {
			t.Errorf("kind %q never emitted", k)
		}
	}
}

// ---- Review round 2, Phase 1: ownership at the point of destruction --------------

func TestAgentAdd_FastPathDoesNotClobberForeignTarget(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	if _, err := a.Add(ctx, app.AddRequest{Root: root, Source: src}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}

	// Hand-written content at the cursor destination.
	foreign := filepath.Join(root, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "NOTES.md"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Pure agent-add: takes the tryLocalAgentAdd fast path.
	_, err := a.Add(ctx, app.AddRequest{Root: root, Source: src, Agents: []string{"cursor"}})
	if err == nil {
		t.Fatal("agent-add clobbered a foreign destination without error")
	}
	if !strings.Contains(err.Error(), foreign) {
		t.Errorf("error does not name the foreign destination: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(foreign, "NOTES.md")); statErr != nil {
		t.Errorf("foreign content destroyed despite the error: %v", statErr)
	}
}

func TestAgentAdd_MergePathDoesNotClobberForeignTarget(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	if _, err := a.Add(ctx, app.AddRequest{Root: root, Source: src}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	foreign := filepath.Join(root, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "NOTES.md"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// --all disqualifies the local fast path, driving the full merge path
	// (planAdd → mergeInto → installSelected → installer).
	_, err := a.Add(ctx, app.AddRequest{Root: root, Source: src, Agents: []string{"cursor"}, All: true})
	if err == nil {
		t.Fatal("merge-path agent-add clobbered a foreign destination without error")
	}
	if _, statErr := os.Stat(filepath.Join(foreign, "NOTES.md")); statErr != nil {
		t.Errorf("foreign content destroyed despite the error: %v", statErr)
	}

	// --force is the documented escape hatch: the overwrite proceeds.
	if _, err := a.Add(ctx, app.AddRequest{Root: root, Source: src, Agents: []string{"cursor"}, All: true, Force: true}); err != nil {
		t.Fatalf("--force agent-add failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".cursor", "skills", "alpha", "SKILL.md")); statErr != nil {
		t.Errorf("forced install did not place the skill: %v", statErr)
	}
}

func TestPlanInstall_CopyModeReonboardIsUpdateNotConflict(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)
	a := onboardApp()
	ctx := context.Background()

	// Install in copy mode, then simulate manifest+lock loss.
	if _, err := a.Add(ctx, app.AddRequest{Root: root, Source: src, Mode: "copy"}); err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	for _, f := range []string{"gskill.toml", "gskill.lock"} {
		if err := os.Remove(filepath.Join(root, f)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Init(ctx, root); err != nil {
		t.Fatal(err)
	}

	// Re-onboarding the same content: the copy-mode dir is gskill's own
	// (hash-matching) content, not a foreign occupant.
	plan := planFor(t, a, root, src, false, "alpha")
	if len(plan.Conflicts) != 0 {
		t.Fatalf("gskill's own copy-mode install flagged as foreign after manifest loss: %+v", plan.Conflicts)
	}
	for _, act := range plan.Actions {
		for _, op := range act.FileOps {
			if filepath.Base(op.Path) == "SKILL.md" && op.Op != "update" {
				t.Errorf("existing owned copy should plan as update, got %+v", op)
			}
		}
	}
}

func TestPlanInstall_ForeignActiveEntryIsConflict(t *testing.T) {
	t.Parallel()

	src := sourceTree(t, "skills/alpha")
	root := projectWithAgent(t)

	// The user's own content occupies the shared active entry.
	activeDir := filepath.Join(root, ".agents", "skills", "alpha")
	if err := os.MkdirAll(activeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "SKILL.md"), []byte("# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := planFor(t, onboardApp(), root, src, false, "alpha")
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != app.ConflictFileOverwrite {
		t.Fatalf("foreign active entry not surfaced pre-approval (FR-016): %+v", plan.Conflicts)
	}
	if !strings.Contains(plan.Conflicts[0].Detail, ".agents") {
		t.Errorf("conflict does not name the active path: %s", plan.Conflicts[0].Detail)
	}
}
