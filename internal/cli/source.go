package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// sourceCmd groups read-only source inspection subcommands.
type sourceCmd struct {
	List    sourceListCmd    `cmd:"" help:"List all skills discovered in a source."`
	Inspect sourceInspectCmd `cmd:"" help:"Show one skill's metadata, path, and diagnostics."`
	Check   sourceCheckCmd   `cmd:"" help:"Report invalid and duplicate skills in a source."`
}

// scanFlags are the discovery filters shared by the source subcommands.
type scanFlags struct {
	Ref      string   `help:"Branch or tag to scan."`
	MaxDepth int      `name:"max-depth" help:"Maximum recursive scan depth (0 = unbounded)."`
	Include  []string `help:"Only discover skills whose in-repo path matches this glob (repeatable)."`
	Exclude  []string `help:"Skip skills whose in-repo path matches this glob (repeatable)."`
}

func (f scanFlags) opts() app.ScanOptions {
	return app.ScanOptions{Ref: f.Ref, MaxDepth: f.MaxDepth, Include: f.Include, Exclude: f.Exclude}
}

// sourceListCmd implements `gskill source list`.
type sourceListCmd struct {
	Source string `arg:"" help:"Skill source: git shorthand, URL, or local path."`
	scanFlags
}

// Help returns the detailed help shown by `gskill source list --help`.
func (sourceListCmd) Help() string {
	return examplesHelp(
		"gskill source list github.com/owner/repo",
		"gskill source list ./local/skills --max-depth 2",
	)
}

// Run lists every discovered skill (FR-032).
func (c sourceListCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	res, err := a.SourceList(ctx, c.Source, c.opts())
	if err != nil {
		return err
	}
	type item struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		RepoPath    string `json:"repo_path"`
		Valid       bool   `json:"valid"`
	}
	items := make([]item, 0, len(res.Skills))
	lines := make([]string, 0, len(res.Skills))
	for _, s := range res.Skills {
		items = append(items, item{s.ID, s.DisplayName, s.Description, s.RepoPath, s.Valid})
		lines = append(lines, fmt.Sprintf("%-30s %-8s %s", s.ID, validity(s.Valid), pathOrRoot(s.RepoPath)))
	}
	return out.Result(strings.Join(lines, "\n"), items)
}

// sourceInspectCmd implements `gskill source inspect`.
type sourceInspectCmd struct {
	Source string `arg:"" help:"Skill source: git shorthand, URL, or local path."`
	Skill  string `required:"" help:"Skill to inspect (name or name@path)."`
	scanFlags
}

// Help returns the detailed help shown by `gskill source inspect --help`.
func (sourceInspectCmd) Help() string {
	return examplesHelp(
		"gskill source inspect github.com/owner/repo --skill deploy-helper",
	)
}

// Run shows one skill's details (FR-033).
func (c sourceInspectCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	insp, err := a.SourceInspect(ctx, c.Source, c.Skill, string(root), c.opts())
	if err != nil {
		return err
	}
	s := insp.Skill
	problems := make([]string, 0, len(s.Problems))
	for _, p := range s.Problems {
		problems = append(problems, fmt.Sprintf("%s: %s", p.Severity, p.Message))
	}
	human := fmt.Sprintf("%s (%s)\n  source:    %s\n  path:      %s\n  valid:     %t\n  agents:    %s\n  problems:  %s",
		s.DisplayName, s.ID, insp.Source, pathOrRoot(s.RepoPath), s.Valid,
		strings.Join(insp.Agents, ", "), strings.Join(problems, "; "))
	return out.Result(human, map[string]any{
		"id": s.ID, "display_name": s.DisplayName, "description": s.Description,
		"repo_path": s.RepoPath, "valid": s.Valid, "source": insp.Source,
		"agents": insp.Agents, "problems": problems,
	})
}

// sourceCheckCmd implements `gskill source check`.
type sourceCheckCmd struct {
	Source string `arg:"" help:"Skill source: git shorthand, URL, or local path."`
	scanFlags
}

// Help returns the detailed help shown by `gskill source check --help`.
func (sourceCheckCmd) Help() string {
	return examplesHelp(
		"gskill source check github.com/owner/repo",
	)
}

// Run reports invalid and duplicate skills, exiting non-zero on problems (FR-034).
func (c sourceCheckCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	report, err := a.SourceCheck(ctx, c.Source, c.opts())
	if err != nil {
		return err
	}

	invalid := make([]map[string]any, 0, len(report.Invalid))
	var lines []string
	for _, s := range report.Invalid {
		problems := make([]string, 0, len(s.Problems))
		for _, p := range s.Problems {
			problems = append(problems, p.Message)
		}
		invalid = append(invalid, map[string]any{"id": s.ID, "repo_path": s.RepoPath, "problems": problems})
		lines = append(lines, fmt.Sprintf("invalid: %s (%s): %s", s.ID, pathOrRoot(s.RepoPath), strings.Join(problems, "; ")))
	}
	dups := make([]map[string]any, 0, len(report.Duplicates))
	for _, d := range report.Duplicates {
		dups = append(dups, map[string]any{"id": d.ID, "paths": d.Paths})
		lines = append(lines, fmt.Sprintf("duplicate: %s at %s", d.ID, strings.Join(d.Paths, ", ")))
	}

	human := "no problems found"
	if report.HasProblems() {
		human = strings.Join(lines, "\n")
	}
	if rErr := out.Result(human, map[string]any{"invalid": invalid, "duplicates": dups}); rErr != nil {
		return rErr
	}
	if report.HasProblems() {
		return errs.New(errs.CodeInvalidManifest, "source has invalid or duplicate skills")
	}
	return nil
}

// validity renders a skill's validity as a short tag.
func validity(valid bool) string {
	if valid {
		return "ok"
	}
	return "invalid"
}

// pathOrRoot renders an in-repo path, showing "." for the root.
func pathOrRoot(p string) string {
	if p == "" {
		return "."
	}
	return p
}
