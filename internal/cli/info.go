package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
)

// infoCmd shows details for one skill.
type infoCmd struct {
	Name string `arg:"" help:"Skill name."`
}

// Run executes `gskill info`.
func (c infoCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	info, err := a.Info(ctx, string(root), c.Name)
	if err != nil {
		return err
	}

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%s (%s)\n", info.Name, info.Version)
	_, _ = fmt.Fprintf(&b, "  source:  %s\n", info.Source)
	_, _ = fmt.Fprintf(&b, "  commit:  %s\n", info.Commit)
	_, _ = fmt.Fprintf(&b, "  content: %s\n", info.ContentHash)
	_, _ = fmt.Fprintf(&b, "  desc:    %s\n", info.Description)
	_, _ = fmt.Fprintf(&b, "  agents:  %s\n", strings.Join(info.Agents, ", "))

	return out.Result(strings.TrimRight(b.String(), "\n"), map[string]any{
		"name":         info.Name,
		"source":       info.Source,
		"version":      info.Version,
		"commit":       info.Commit,
		"content_hash": info.ContentHash,
		"description":  info.Description,
		"license":      info.License,
		"agents":       info.Agents,
		"targets":      info.Targets,
		"requires": map[string]any{
			"skills":      info.Requires.Skills,
			"commands":    info.Requires.Commands,
			"environment": info.Requires.Environment,
			"mcp":         info.Requires.MCP,
		},
	})
}
