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
			Name:    "kubernetes-expert",
			Source:  "github.com/acme/widgets",
			Version: "2.1.3",
			Status:  "installed",
			Agents:  []string{"claude", "codex"},
		},
		{
			Name:    "shell-helper",
			Source:  "github.com/acme/shell",
			Version: "1.0.0",
			Status:  "missing",
			Agents:  []string{"claude"},
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
