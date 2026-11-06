package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/testutil"
)

// Repositories of the golden plan: two remote groups and a local one. deploy
// spells its repository differently, so the goldens show that JSON names the
// group's label, not each skill's own spelling.
var (
	goldenAcme   = source.Repo{Kind: source.RepoRemote, Key: "github.com/acme/skills", Label: "github.com/acme/skills"}
	goldenAcmeUC = source.Repo{Kind: source.RepoRemote, Key: "github.com/acme/skills", Label: "github.com/Acme/Skills"}
	goldenOps    = source.Repo{Kind: source.RepoRemote, Key: "gitlab.com/acme/ops", Label: "gitlab.com/acme/ops"}
	goldenTools  = source.Repo{Kind: source.RepoLocal, Key: "/project/tools", Label: "../tools"}
)

// matrixPlan is a fixed update plan covering every status the report renders.
func matrixPlan() app.UpdatePlan {
	return app.UpdatePlan{Items: []app.UpdatePlanItem{
		{
			Repo: goldenAcme,
			Name: "cdep", Source: "github.com/acme/cdep", Current: "eeeeeeeeeeee",
			Policy: "commit:eeeeeeeeeeee", Status: app.StatusPinnedCommit,
			Reason: "pinned to an exact commit; not changed by a normal update",
		},
		{
			Repo: goldenAcmeUC,
			Name: "deploy", Source: "github.com/acme/deploy", Current: "v1.0.0",
			Policy: "tag:v1.0.0", Status: app.StatusPinnedTag,
			Reason:        "pinned to an exact tag; not changed by a normal update",
			Informational: "2.0.0",
		},
		{
			Repo: goldenOps,
			Name: "kubernetes", Source: "github.com/acme/k8s", Current: "1.2.0",
			Candidate: "1.4.1", Policy: "^1.2.0", Status: app.StatusUpdateAvailable,
		},
		{
			Repo: goldenAcme,
			Name: "legacy", Source: "github.com/acme/legacy", Current: "1.3.0",
			Policy: "^1.0.0", Status: app.StatusNoCompatibleUpdate,
			Reason:        "newer releases exist outside the version constraint",
			Informational: "2.0.0",
		},
		{
			Repo: goldenTools,
			Name: "local-tools", Source: "../tools", Current: "local",
			Policy: "local", Status: app.StatusLocalSource,
			Reason: "local source has no remote update candidate",
		},
		{
			Repo: goldenOps,
			Name: "security", Source: "github.com/acme/sec", Current: "2.3.2",
			Policy: "^2.0.0", Status: app.StatusUpToDate,
			Reason: "already the newest revision the policy allows",
		},
	}}
}

// Every update result names its group's repository and whether it is local
// (spec 032 FR-030).
func TestUpdateResultJSON_NamesRepository(t *testing.T) {
	t.Parallel()

	res := app.UpdateResult{Skills: []app.UpdateSkillResult{
		{Name: "a", Outcome: app.UpdateOutcomeUpdated, Repo: goldenAcme},
		{Name: "b", Outcome: app.UpdateOutcomeNoChange, Repo: goldenAcmeUC},
		{Name: "c", Outcome: app.UpdateOutcomeNoChange, Repo: goldenTools},
	}}
	skills, ok := cli.UpdateResultJSON(res)["skills"].([]map[string]any)
	if !ok || len(skills) != 3 {
		t.Fatalf("skills = %#v", cli.UpdateResultJSON(res)["skills"])
	}
	want := []struct {
		repo  string
		local bool
	}{{"github.com/acme/skills", false}, {"github.com/acme/skills", false}, {"../tools", true}}
	for i, w := range want {
		if skills[i]["repository"] != w.repo || skills[i]["local"] != w.local {
			t.Errorf("%s: repository %v local %v, want %q %v", skills[i]["name"], skills[i]["repository"], skills[i]["local"], w.repo, w.local)
		}
	}
}

func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	testutil.Golden(t, name, buf.Bytes())
}

// The update report JSON contracts are stable: candidate/status fields per
// skill and an updates_available count (contracts/cli-update.md).
func TestUpdateListJSON_StableGolden(t *testing.T) {
	t.Parallel()
	goldenJSON(t, "update-list-json.golden", cli.UpdateListJSON(matrixPlan(), false))
	goldenJSON(t, "update-list-all-json.golden", cli.UpdateListJSON(matrixPlan(), true))
}

// The outdated JSON keeps its historical fields (name/current/latest/
// available/any_available) with plan detail added additively (FR-016).
func TestOutdatedJSON_StableGolden(t *testing.T) {
	t.Parallel()
	goldenJSON(t, "outdated-json.golden", cli.OutdatedJSON(matrixPlan()))
}
