package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/testutil"
)

// renderJSON captures the --json document for a fabricated result.
func renderJSON(t *testing.T, res app.InstallFromLockResult) []byte {
	t.Helper()
	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, OutputOptions{JSON: true})
	if err := renderLockInstall(out, res, false); err != nil {
		t.Fatalf("renderLockInstall: %v", err)
	}
	return outb.Bytes()
}

// TestInstallJSON_Goldens (FR-023, SC-005): the document shape is stable and
// pinned — status, full summary counters, and per-skill structured results.
func TestInstallJSON_Goldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		golden string
		res    app.InstallFromLockResult
	}{
		{
			name:   "success",
			golden: "install-json-success.golden",
			res: app.InstallFromLockResult{
				Agents: []string{"claude"}, Changed: true,
				Skills: []app.LockSkillResult{
					successResult("alpha", app.LockSkillInstalled),
					successResult("beta", app.LockSkillUpToDate),
				},
			},
		},
		{
			name:   "partial",
			golden: "install-json-partial.golden",
			res:    partialResults(),
		},
		{
			name:   "total-failure",
			golden: "install-json-total-failure.golden",
			res: app.InstallFromLockResult{
				Agents: []string{"claude"},
				Skills: []app.LockSkillResult{
					renderFailure("alpha", "github.com/acme/skills", "",
						app.FailureAuthentication, app.InstallPhaseFetching,
						"authentication failure: repository requires credentials",
						"configure a token and retry"),
				},
			},
		},
		{
			name:   "cancelled",
			golden: "install-json-cancelled.golden",
			res:    cancelledResults(),
		},
		{
			name:   "dry-run",
			golden: "install-json-dry-run.golden",
			res: app.InstallFromLockResult{
				Agents: []string{"claude"},
				Skills: []app.LockSkillResult{
					{Name: "fresh", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldInstall},
					{Name: "rewrite", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldUpdateLock},
				},
			},
		},
		{
			// A failure at resolving has no resolved version/ref/commit: the
			// keys must be absent, never fabricated (FR-014's JSON analogue).
			name:   "missing-version",
			golden: "install-json-missing-version.golden",
			res: app.InstallFromLockResult{
				Agents: []string{"claude"},
				Skills: []app.LockSkillResult{
					renderFailure("early", "github.com/acme/skills", "",
						app.FailureResolution, app.InstallPhaseResolving,
						"no such ref v9.9.9", ""),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testutil.Golden(t, tt.golden, renderJSON(t, tt.res))
		})
	}
}

// installJSONDoc mirrors the documented shape for decoding assertions.
type installJSONDoc struct {
	Status  string         `json:"status"`
	Summary map[string]int `json:"summary"`
	Skills  []struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		SourceType      string `json:"sourceType"`
		ResolvedVersion string `json:"resolvedVersion"`
		Commit          string `json:"commit"`
		Phase           string `json:"phase"`
		Failure         *struct {
			Category string `json:"category"`
			Phase    string `json:"phase"`
			Message  string `json:"message"`
			Hint     string `json:"hint"`
		} `json:"failure"`
	} `json:"skills"`
}

// TestInstallJSON_SummaryInvariantAndFailureShape decodes the partial
// document and asserts the counter invariant plus the failure object rules.
func TestInstallJSON_SummaryInvariantAndFailureShape(t *testing.T) {
	t.Parallel()
	var doc installJSONDoc
	if err := json.Unmarshal(renderJSON(t, partialResults()), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Status != "partial" {
		t.Errorf("status = %q, want partial", doc.Status)
	}
	sum := 0
	for k, v := range doc.Summary {
		if k != "total" {
			sum += v
		}
	}
	if doc.Summary["total"] != sum {
		t.Errorf("summary total = %d, counters sum to %d (FR-015)", doc.Summary["total"], sum)
	}
	for _, k := range []string{"total", "installed", "repaired", "upToDate", "skipped", "failed", "cancelled", "notAttempted"} {
		if _, ok := doc.Summary[k]; !ok {
			t.Errorf("summary missing counter %q", k)
		}
	}
	for _, s := range doc.Skills {
		if (s.Failure != nil) != (s.Status == "failed") {
			t.Errorf("skill %s: failure object present=%v with status %q (want present ⇔ failed)",
				s.Name, s.Failure != nil, s.Status)
		}
		if s.Failure != nil && (s.Failure.Category == "" || s.Failure.Message == "") {
			t.Errorf("skill %s: failure lacks category/message: %+v", s.Name, s.Failure)
		}
	}
}

// TestInstallJSON_UnknownScalarsOmitted: a resolving-phase failure carries no
// resolvedVersion/commit keys at all.
func TestInstallJSON_UnknownScalarsOmitted(t *testing.T) {
	t.Parallel()
	raw := renderJSON(t, app.InstallFromLockResult{
		Skills: []app.LockSkillResult{
			renderFailure("early", "src", "", app.FailureResolution,
				app.InstallPhaseResolving, "no such ref", ""),
		},
	})
	var generic struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"resolvedVersion", "resolvedRef", "commit"} {
		if _, ok := generic.Skills[0][key]; ok {
			t.Errorf("unresolved skill carries %q key (must be omitted, not fabricated)", key)
		}
	}
	// Preserved legacy fields stay present unconditionally.
	for _, key := range []string{"name", "source", "status", "computedHash"} {
		if _, ok := generic.Skills[0][key]; !ok {
			t.Errorf("legacy field %q missing (stability contract)", key)
		}
	}
}

// hostileResults carries ANSI/OSC injection through every untrusted field.
func hostileResults() app.InstallFromLockResult {
	return app.InstallFromLockResult{Skills: []app.LockSkillResult{
		renderFailure("evil\x1b]0;pwned\x07", "src\x1b[2Jwipe", "v1\x1b[31m",
			app.FailureUnknown, app.InstallPhaseVerifying,
			"boom \x1b[31mred\x1b[0m", "hint\x1b]8;;x\x07"),
	}}
}

// TestInstallJSON_HostileInputGolden (SC-008, JSON path): control characters
// in untrusted metadata are escaped by encoding/json — pinned by a golden so
// the escaping behavior can never silently regress.
func TestInstallJSON_HostileInputGolden(t *testing.T) {
	t.Parallel()
	testutil.Golden(t, "install-json-hostile.golden", renderJSON(t, hostileResults()))
}

// TestInstallJSON_NoRawControlBytes: regardless of golden content, the raw
// document must never contain unescaped ESC/BEL bytes a terminal could act
// on when the JSON is cat'ed.
func TestInstallJSON_NoRawControlBytes(t *testing.T) {
	t.Parallel()
	raw := renderJSON(t, hostileResults())
	if bytes.ContainsAny(raw, "\x1b\x07") {
		t.Errorf("JSON output carries raw control bytes:\n%q", raw)
	}
}
