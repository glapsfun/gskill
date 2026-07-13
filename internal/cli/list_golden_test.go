package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/testutil"
)

func TestListJSON_StableGolden(t *testing.T) {
	t.Parallel()

	skills := []app.ListedSkill{
		{
			Name:        "kubernetes-expert",
			Source:      "github.com/acme/widgets",
			Version:     "2.1.3",
			Status:      "installed",
			Agents:      []string{"claude", "codex"},
			Commit:      "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
			ContentHash: "sha256:deadbeefcafef00d",
			Active:      "ok",
			AgentHealth: []app.AgentHealthEntry{
				{ID: "claude", Mode: "symlink", Health: "ok-symlink"},
				{ID: "codex", Mode: "symlink", Health: "missing"},
			},
		},
		{
			Name:        "shell-helper",
			Source:      "github.com/acme/shell",
			Version:     "1.0.0",
			Status:      "missing",
			Agents:      []string{"claude"},
			Active:      "missing",
			AgentHealth: []app.AgentHealthEntry{},
		},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cli.ListJSON(skills)); err != nil {
		t.Fatalf("encode: %v", err)
	}

	testutil.Golden(t, "list.golden", buf.Bytes())
}

// TestListJSON_EmptyUnchanged verifies the zero-skills JSON empty-state
// stays `{"skills": []}` after the merge (spec FR-009).
func TestListJSON_EmptyUnchanged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cli.ListJSON(nil)); err != nil {
		t.Fatalf("encode: %v", err)
	}

	want := "{\"skills\":[]}\n"
	if got := buf.String(); got != want {
		t.Errorf("ListJSON(nil) = %q, want %q", got, want)
	}
}
