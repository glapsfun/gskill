package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/tui"
)

// addCmd adds and installs one or more skills discovered in a source.
type addCmd struct {
	Source  string   `arg:"" help:"Skill source: git shorthand, URL, or local path."`
	Skill   []string `help:"Select a discovered skill by name (repeatable; '*' selects all; name@path disambiguates)."`
	All     bool     `help:"Select every valid discovered skill."`
	List    bool     `help:"List discovered skills without installing."`
	Path    string   `help:"In-repo path disambiguator for a duplicated --skill."`
	Version string   `help:"Semver constraint (e.g. ^2.0.0)."`
	Ref     string   `help:"Branch or tag to track."`
	Commit  string   `help:"Explicit commit SHA to pin."`
	Exact   bool     `help:"Pin to the exact resolved version."`
	Agent   []string `help:"Target agent ID (repeatable)."`
	Force   bool     `help:"Overwrite an existing declaration and re-resolve."`
	Global  bool     `help:"Install into the user-global location."`
	Project bool     `help:"Install into the project (default)."`
	Auto    bool     `xor:"installmode" help:"Prefer a symlink, fall back to a copy (default)."`
	Copy    bool     `xor:"installmode" help:"Copy instead of linking."`
	Symlink bool     `xor:"installmode" help:"Symlink, never copy."`

	MaxDepth int      `name:"max-depth" help:"Maximum recursive scan depth (0 = unbounded)."`
	Include  []string `help:"Only discover skills whose in-repo path matches this glob (repeatable)."`
	Exclude  []string `help:"Skip skills whose in-repo path matches this glob (repeatable)."`
}

// Help returns the detailed help shown by `gskill add --help`.
func (addCmd) Help() string {
	return examplesHelp(
		"gskill add github.com/owner/repo --agent claude",
		"gskill add github.com/owner/repo --skill deploy-helper --version '^2.0.0'",
		"gskill add ./local/skills --all",
		"gskill add github.com/owner/repo --list",
	)
}

// Run executes `gskill add`.
func (c addCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Add(ctx, app.AddRequest{
		Root:        string(root),
		Source:      c.Source,
		Version:     c.Version,
		Ref:         c.Ref,
		Commit:      c.Commit,
		Agents:      c.Agent,
		Force:       c.Force,
		Scope:       scopeFlag(c.Global),
		Mode:        modeFromFlags(c.Copy, c.Symlink, c.Auto),
		Selectors:   c.Skill,
		All:         c.All,
		Path:        c.Path,
		ListOnly:    c.List,
		Interactive: out.Interactive(),
		MaxDepth:    c.MaxDepth,
		Include:     c.Include,
		Exclude:     c.Exclude,
		Chooser:     skillChooser(out.Interactive()),
	})
	if err != nil {
		return err
	}

	for _, w := range res.Warnings {
		out.Diag("warning: %s", w)
	}

	if c.List {
		return c.renderList(out, res)
	}
	return c.renderInstalled(out, res)
}

// renderList prints the discovered skills without installing.
func (c addCmd) renderList(out *Output, res app.AddResult) error {
	type listItem struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		RepoPath    string `json:"repo_path"`
		Valid       bool   `json:"valid"`
	}
	items := make([]listItem, 0, len(res.Listed))
	lines := make([]string, 0, len(res.Listed))
	for _, s := range res.Listed {
		items = append(items, listItem{s.ID, s.DisplayName, s.Description, s.RepoPath, s.Valid})
		mark := "ok"
		if !s.Valid {
			mark = "invalid"
		}
		path := s.RepoPath
		if path == "" {
			path = "."
		}
		lines = append(lines, fmt.Sprintf("%-30s %-10s %s", s.ID, mark, path))
	}
	return out.Result(strings.Join(lines, "\n"), items)
}

// renderInstalled prints the installed skills.
func (c addCmd) renderInstalled(out *Output, res app.AddResult) error {
	names := make([]string, 0, len(res.Installed))
	for _, s := range res.Installed {
		names = append(names, s.Name)
	}
	human := fmt.Sprintf("Installed %d skill(s): %s", len(res.Installed), strings.Join(names, ", "))
	return out.Result(human, map[string]any{
		"installed": res.Installed,
		"warnings":  res.Warnings,
	})
}

// skillChooser returns an interactive picker backed by the TUI multi-select,
// mapping the discovered skills to selectable items and back. It returns nil
// when not interactive, so the app falls back to the non-interactive error.
func skillChooser(interactive bool) func([]discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error) {
	if !interactive {
		return nil
	}
	return func(cands []discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error) {
		items := make([]tui.SkillItem, len(cands))
		for i, s := range cands {
			items[i] = tui.SkillItem{ID: s.ID, DisplayName: s.DisplayName, RepoPath: s.RepoPath, Valid: s.Valid}
		}
		idx, err := tui.SelectSkills(items, true)
		if err != nil {
			return nil, err
		}
		chosen := make([]discovery.DiscoveredSkill, 0, len(idx))
		for _, i := range idx {
			chosen = append(chosen, cands[i])
		}
		return chosen, nil
	}
}

// scopeFlag maps the --global flag to a scope string.
func scopeFlag(global bool) string {
	if global {
		return string(installer.ScopeGlobal)
	}
	return string(installer.ScopeProject)
}

// modeFromFlags resolves --copy/--symlink/--auto to an install-mode preference
// string ("" means default, which resolves to auto). The flags are mutually
// exclusive at the parser level (kong xor), so at most one is set here.
func modeFromFlags(copyMode, symlinkMode, autoMode bool) string {
	switch {
	case copyMode:
		return installer.PrefCopy
	case symlinkMode:
		return installer.PrefSymlink
	case autoMode:
		return installer.PrefAuto
	default:
		return ""
	}
}
