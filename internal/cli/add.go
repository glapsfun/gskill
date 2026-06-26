package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/installer"
)

// addCmd adds and installs a new skill.
type addCmd struct {
	Source  string   `arg:"" help:"Skill source: git shorthand, URL, or local path."`
	Version string   `help:"Semver constraint (e.g. ^2.0.0)."`
	Ref     string   `help:"Branch or tag to track."`
	Commit  string   `help:"Explicit commit SHA to pin."`
	Exact   bool     `help:"Pin to the exact resolved version."`
	Agent   []string `help:"Target agent ID (repeatable)."`
	Force   bool     `help:"Overwrite an existing declaration and re-resolve."`
	Global  bool     `help:"Install into the user-global location."`
	Copy    bool     `help:"Copy instead of symlinking."`
}

// Run executes `gskill add`.
func (c addCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Add(ctx, app.AddRequest{
		Root:    string(root),
		Source:  c.Source,
		Version: c.Version,
		Ref:     c.Ref,
		Commit:  c.Commit,
		Agents:  c.Agent,
		Force:   c.Force,
		Scope:   scopeFlag(c.Global),
		Mode:    modeFlag(c.Copy),
	})
	if err != nil {
		return err
	}

	for _, w := range res.Warnings {
		out.Diag("warning: %s", w)
	}

	human := fmt.Sprintf("Added %s (%s) into %d agent(s)", res.Name, res.ContentHash, len(res.Targets))
	return out.Result(human, map[string]any{
		"name":         res.Name,
		"content_hash": res.ContentHash,
		"targets":      res.Targets,
		"warnings":     res.Warnings,
	})
}

// scopeFlag maps the --global flag to a scope string.
func scopeFlag(global bool) string {
	if global {
		return string(installer.ScopeGlobal)
	}
	return string(installer.ScopeProject)
}

// modeFlag maps the --copy flag to an install-mode string ("" means default).
func modeFlag(copyMode bool) string {
	if copyMode {
		return string(installer.ModeCopy)
	}
	return ""
}
