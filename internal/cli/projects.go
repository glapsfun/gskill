package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/tui"
)

// projectsCmd groups advisory project-registry subcommands (spec 015 FR-028).
type projectsCmd struct {
	List    projectsListCmd    `cmd:"" help:"List projects known to use the global store."`
	Inspect projectsInspectCmd `cmd:"" help:"Show one registered project's details."`
	Prune   projectsPruneCmd   `cmd:"" help:"Drop registry entries for projects that no longer exist."`
	Refresh projectsRefreshCmd `cmd:"" help:"Re-derive every registry entry from its project."`
}

// Help returns the detailed help shown by `gskill projects --help`.
func (projectsCmd) Help() string {
	return examplesHelp("gskill projects list", "gskill projects prune")
}

type projectsListCmd struct{}

// Help returns the detailed help shown by `gskill projects list --help`.
func (projectsListCmd) Help() string {
	return examplesHelp("gskill projects list", "gskill projects list --json")
}

// Run lists the registry. The registry is advisory: deleting it never breaks
// a project (FR-027).
func (projectsListCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	infos, err := a.ProjectsList(ctx)
	if err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(infos))
	var b strings.Builder
	fmt.Fprintf(&b, "%-34s %-7s %-12s %s\n", "Project", "Skills", "Last seen", "Path")
	for _, info := range infos {
		liveness := humanSince(info.LastSeen)
		if info.Missing {
			liveness = "missing"
		}
		path := info.Root
		if path == "" {
			path = "-"
		}
		fmt.Fprintf(&b, "%-34s %-7d %-12s %s\n",
			tui.Sanitize(info.ProjectID), info.Skills, liveness, tui.Sanitize(path))
		row := map[string]any{
			"projectId": info.ProjectID,
			"skills":    info.Skills,
			"lastSeen":  info.LastSeen.Format(time.RFC3339),
			"missing":   info.Missing,
		}
		if info.Root != "" {
			row["root"] = info.Root
		}
		rows = append(rows, row)
	}
	if len(infos) == 0 {
		b.Reset()
		b.WriteString("no projects registered")
	}
	return out.Result(strings.TrimRight(b.String(), "\n"), map[string]any{"projects": rows})
}

// humanSince renders a coarse relative time.
func humanSince(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	d := time.Since(ts)
	switch {
	case d < 24*time.Hour:
		return "today"
	case d < 48*time.Hour:
		return "yesterday"
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

type projectsInspectCmd struct {
	ProjectID string `arg:"" help:"Registered project identifier."`
}

// Help returns the detailed help shown by `gskill projects inspect --help`.
func (projectsInspectCmd) Help() string {
	return examplesHelp("gskill projects inspect p-abc123…")
}

// Run shows one registry entry.
func (c projectsInspectCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	e, err := a.ProjectsInspect(ctx, c.ProjectID)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Project %s\n", tui.Sanitize(e.ProjectID))
	fmt.Fprintf(&b, "  Root:      %s\n", tui.OrDash(e.Root))
	fmt.Fprintf(&b, "  Lockfile:  %s\n", tui.OrDash(e.Lockfile))
	fmt.Fprintf(&b, "  Last seen: %s", e.LastSeen.Format(time.RFC3339))
	for _, ref := range e.References {
		fmt.Fprintf(&b, "\n  skill %s -> %s", tui.Sanitize(ref.Skill), tui.Sanitize(ref.StoreHash))
	}
	refs := make([]map[string]any, 0, len(e.References))
	for _, ref := range e.References {
		refs = append(refs, map[string]any{"skill": ref.Skill, "storeHash": ref.StoreHash})
	}
	doc := map[string]any{
		"projectId":  e.ProjectID,
		"lastSeen":   e.LastSeen.Format(time.RFC3339),
		"references": refs,
	}
	if e.Root != "" {
		doc["root"] = e.Root
		doc["lockfile"] = e.Lockfile
	}
	return out.Result(b.String(), doc)
}

type projectsPruneCmd struct{}

// Help returns the detailed help shown by `gskill projects prune --help`.
func (projectsPruneCmd) Help() string {
	return examplesHelp("gskill projects prune")
}

// Run removes stale registry entries only — never repository files (FR-028).
func (projectsPruneCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	removed, err := a.ProjectsPrune(ctx)
	if err != nil {
		return err
	}
	human := "nothing to prune"
	if len(removed) > 0 {
		human = "pruned registry entries: " + tui.Sanitize(strings.Join(removed, ", "))
	}
	return out.Result(human, map[string]any{"pruned": nonNilStrings(removed)})
}

type projectsRefreshCmd struct{}

// Help returns the detailed help shown by `gskill projects refresh --help`.
func (projectsRefreshCmd) Help() string {
	return examplesHelp("gskill projects refresh")
}

// Run re-derives registry entries from their projects.
func (projectsRefreshCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	refreshed, err := a.ProjectsRefresh(ctx)
	if err != nil {
		return err
	}
	human := "nothing to refresh"
	if len(refreshed) > 0 {
		human = fmt.Sprintf("refreshed %d registry entries", len(refreshed))
	}
	return out.Result(human, map[string]any{"refreshed": nonNilStrings(refreshed)})
}
