package cli

import (
	"context"
	"testing"

	"github.com/glapsfun/gskill/internal/tui"
)

// TestDashboardService_InventoryMapsListAndPlan checks the adapter builds a
// DashboardSkill per installed skill, carrying the agents and a health status,
// with a repository identity for grouping (spec 033 T012, FR-001). It runs
// offline so discovery needs no network.
func TestDashboardService_InventoryMapsListAndPlan(t *testing.T) {
	t.Parallel()
	dir := installedAlphaProject(t)
	svc := &dashboardService{a: newTestApp(), root: dir, offline: true}

	skills, err := svc.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("Inventory returned no skills")
	}
	var alpha tui.DashboardSkill
	for _, got := range skills {
		if got.Name == "alpha" {
			alpha = got
		}
	}
	if alpha.Name != "alpha" {
		t.Fatalf("installed skill 'alpha' not found in inventory of %d", len(skills))
	}
	if alpha.Health == "" {
		t.Error("health (STATUS column) is empty")
	}
	if len(alpha.Agents) == 0 || alpha.Agents[0] != "claude" {
		t.Errorf("agents = %v, want [claude]", alpha.Agents)
	}
	if alpha.SourceLabel == "" {
		t.Error("source label (grouping) is empty")
	}
	if alpha.Markdown == "" {
		t.Error("SKILL.md preview was not preloaded")
	}
}
