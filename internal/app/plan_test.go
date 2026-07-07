package app_test

import (
	"context"
	"os"
	"path/filepath"
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
