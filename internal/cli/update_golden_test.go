package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/testutil"
)

// matrixPlan is a fixed update plan covering every status the report renders.
func matrixPlan() app.UpdatePlan {
	return app.UpdatePlan{Items: []app.UpdatePlanItem{
		{
			Name: "cdep", Source: "github.com/acme/cdep", Current: "eeeeeeeeeeee",
			Policy: "commit:eeeeeeeeeeee", Status: app.StatusPinnedCommit,
			Reason: "pinned to an exact commit; not changed by a normal update",
		},
		{
			Name: "deploy", Source: "github.com/acme/deploy", Current: "v1.0.0",
			Policy: "tag:v1.0.0", Status: app.StatusPinnedTag,
			Reason:        "pinned to an exact tag; not changed by a normal update",
			Informational: "2.0.0",
		},
		{
			Name: "kubernetes", Source: "github.com/acme/k8s", Current: "1.2.0",
			Candidate: "1.4.1", Policy: "^1.2.0", Status: app.StatusUpdateAvailable,
		},
		{
			Name: "legacy", Source: "github.com/acme/legacy", Current: "1.3.0",
			Policy: "^1.0.0", Status: app.StatusNoCompatibleUpdate,
			Reason:        "newer releases exist outside the version constraint",
			Informational: "2.0.0",
		},
		{
			Name: "local-tools", Source: "../tools", Current: "local",
			Policy: "local", Status: app.StatusLocalSource,
			Reason: "local source has no remote update candidate",
		},
		{
			Name: "security", Source: "github.com/acme/sec", Current: "2.3.2",
			Policy: "^2.0.0", Status: app.StatusUpToDate,
			Reason: "already the newest revision the policy allows",
		},
	}}
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
