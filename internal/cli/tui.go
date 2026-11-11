package cli

import (
	"context"
	"fmt"
	"sort"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/tui"
)

// tuiCmd launches the interactive dashboard. Its canonical command name is
// `dashboard`; the original `tui` remains a kong alias.
type tuiCmd struct{}

// Help returns the detailed help shown by `gskill dashboard --help`.
func (tuiCmd) Help() string {
	return examplesHelp("gskill dashboard")
}

// Run executes `gskill dashboard` (alias: `gskill tui`). It wires an app-backed
// DashboardService and hands it to the TUI; the dashboard performs no mutation
// of its own (spec 033 FR-021).
func (tuiCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	svc := &dashboardService{a: a, root: string(root), offline: g.Offline, noCache: g.NoCache}
	return tui.Run(ctx, svc, out.Interactive(), tui.Options{Offline: g.Offline})
}

// dashboardService adapts *app.App to tui.DashboardService. It forwards the
// CLI globals (offline, no-cache) and keeps all policy, locking, and rollback
// in the application layer; a dashboard action has the same effect as the
// matching CLI command (FR-021).
type dashboardService struct {
	a       *app.App
	root    string
	offline bool
	noCache bool
}

// Inventory lists installed skills enriched with their update plan, grouped by
// repository. SKILL.md is pre-loaded for the preview.
func (s *dashboardService) Inventory(ctx context.Context) ([]tui.DashboardSkill, error) {
	listed, err := s.a.List(ctx, s.root)
	if err != nil {
		return nil, err
	}
	plan, err := s.a.PlanUpdate(ctx, app.UpdatePlanRequest{Root: s.root, Offline: s.offline, NoCache: s.noCache})
	if err != nil {
		return nil, err
	}
	items := make(map[string]app.UpdatePlanItem, len(plan.Items))
	for _, it := range plan.Items {
		items[it.Name] = it
	}
	out := make([]tui.DashboardSkill, 0, len(listed))
	for _, l := range listed {
		it := items[l.Name]
		md, mdErr := s.a.SkillMarkdown(ctx, s.root, l.Name)
		if mdErr != nil {
			md = "# " + l.Name + "\n\n_" + l.Status + "_\n"
		}
		out = append(out, tui.DashboardSkill{
			Name:             l.Name,
			Repo:             it.Repo,
			SourceLabel:      labelOr(it.Repo.Label, l.Source),
			Path:             it.Path,
			InstalledVersion: l.Version,
			Candidate:        it.Candidate,
			Health:           l.Status,
			UpdateStatus:     string(it.Status),
			Reason:           it.Reason,
			Policy:           it.Policy,
			Agents:           l.Agents,
			Informational:    it.Informational,
			Markdown:         md,
		})
	}
	return out, nil
}

// Markdown returns a skill's installed SKILL.md.
func (s *dashboardService) Markdown(ctx context.Context, name string) (string, error) {
	return s.a.SkillMarkdown(ctx, s.root, name)
}

// Versions lists the selectable version labels for a skill's source.
func (s *dashboardService) Versions(ctx context.Context, name string) ([]string, error) {
	info, err := s.a.Info(ctx, s.root, name)
	if err != nil {
		return nil, err
	}
	vl, err := s.a.ListVersions(ctx, s.root, info.Source, s.offline)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vl.Candidates))
	for _, c := range vl.Candidates {
		out = append(out, c.Label)
	}
	return out, nil
}

// PlanUpdate previews a compatible update for one skill.
func (s *dashboardService) PlanUpdate(ctx context.Context, name string) (tui.ProposedChange, error) {
	plan, err := s.a.PlanUpdate(ctx, app.UpdatePlanRequest{
		Root: s.root, Names: []string{name}, Offline: s.offline, NoCache: s.noCache,
	})
	if err != nil {
		return tui.ProposedChange{}, err
	}
	for _, it := range plan.Items {
		if it.Name == name {
			return tui.ProposedChange{
				Kind: tui.ChangeUpdate, Target: name, FromRev: it.Current, ToRev: it.Candidate,
			}, nil
		}
	}
	return tui.ProposedChange{}, fmt.Errorf("skill %q not found in update plan", name)
}

// PlanVersionChange previews an explicit version change, including the
// skills.toml declaration before and after.
func (s *dashboardService) PlanVersionChange(ctx context.Context, name, target string, latest bool) (tui.ProposedChange, error) {
	plan, err := s.a.PlanUpgrade(ctx, app.UpgradeRequest{
		Root: s.root, Names: []string{name}, To: target, Latest: latest, Offline: s.offline, NoCache: s.noCache,
	})
	if err != nil {
		return tui.ProposedChange{}, err
	}
	for _, it := range plan.Items {
		if it.Name == name {
			return tui.ProposedChange{
				Kind: tui.ChangeVersion, Target: name, Version: target, Latest: latest,
				FromRev: it.Current, ToRev: candidateLabel(it.Candidate), DeclBefore: it.CurrentDecl, DeclAfter: it.NewDecl,
			}, nil
		}
	}
	return tui.ProposedChange{}, fmt.Errorf("skill %q not found in upgrade plan", name)
}

// PlanRemove previews a removal and its affected installations.
func (s *dashboardService) PlanRemove(ctx context.Context, name string) (tui.ProposedChange, error) {
	info, err := s.a.Info(ctx, s.root, name)
	if err != nil {
		return tui.ProposedChange{}, err
	}
	agents := make([]string, 0, len(info.Targets))
	for agent := range info.Targets {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	installs := make([]string, 0, len(agents))
	for _, agent := range agents {
		installs = append(installs, agent+": "+info.Targets[agent])
	}
	return tui.ProposedChange{
		Kind: tui.ChangeRemove, Target: name, FromRev: info.Version, Installations: installs,
	}, nil
}

// ApplyUpdate applies a compatible update to one skill.
func (s *dashboardService) ApplyUpdate(ctx context.Context, name string) ([]tui.ActionOutcome, error) {
	res, err := s.a.Update(ctx, app.UpdateRequest{Root: s.root, Names: []string{name}, Offline: s.offline, NoCache: s.noCache})
	return updateOutcomes(res, tui.ChangeUpdate), err
}

// ApplyGroupUpdate applies a compatible update to every eligible skill in a
// repository group.
func (s *dashboardService) ApplyGroupUpdate(ctx context.Context, repoKey string) ([]tui.ActionOutcome, error) {
	res, err := s.a.Update(ctx, app.UpdateRequest{Root: s.root, Repos: []string{repoKey}, Offline: s.offline, NoCache: s.noCache})
	return updateOutcomes(res, tui.ChangeGroupUpdate), err
}

// ApplyVersionChange applies an explicit version change to one skill.
func (s *dashboardService) ApplyVersionChange(ctx context.Context, name, target string, latest bool) ([]tui.ActionOutcome, error) {
	res, err := s.a.Upgrade(ctx, app.UpgradeRequest{
		Root: s.root, Names: []string{name}, To: target, Latest: latest, Offline: s.offline, NoCache: s.noCache,
	})
	out := make([]tui.ActionOutcome, 0, len(res.Skills))
	for _, sk := range res.Skills {
		out = append(out, tui.ActionOutcome{Name: sk.Name, Kind: tui.ChangeVersion, Result: string(sk.Outcome), Detail: outcomeDetail(sk.Reason, sk.Err)})
	}
	return out, err
}

// ApplyRemove removes one skill.
func (s *dashboardService) ApplyRemove(ctx context.Context, name string) ([]tui.ActionOutcome, error) {
	res, err := s.a.Remove(ctx, s.root, []string{name})
	if err != nil {
		return []tui.ActionOutcome{{Name: name, Kind: tui.ChangeRemove, Result: "failed", Detail: err.Error()}}, err
	}
	out := make([]tui.ActionOutcome, 0, len(res.Removed)+len(res.NotPresent))
	for _, n := range res.Removed {
		out = append(out, tui.ActionOutcome{Name: n, Kind: tui.ChangeRemove, Result: "removed"})
	}
	for _, n := range res.NotPresent {
		out = append(out, tui.ActionOutcome{Name: n, Kind: tui.ChangeRemove, Result: "not-present"})
	}
	return out, nil
}

// updateOutcomes maps an UpdateResult to dashboard outcomes.
func updateOutcomes(res app.UpdateResult, kind tui.ChangeKind) []tui.ActionOutcome {
	out := make([]tui.ActionOutcome, 0, len(res.Skills))
	for _, sk := range res.Skills {
		out = append(out, tui.ActionOutcome{Name: sk.Name, Kind: kind, Result: string(sk.Outcome), Detail: outcomeDetail(sk.Reason, sk.Err)})
	}
	return out
}

// outcomeDetail prefers a concrete error over the plan's reason text.
func outcomeDetail(reason string, err error) string {
	if err != nil {
		return err.Error()
	}
	return reason
}

// candidateLabel renders an upgrade candidate's target revision for display.
func candidateLabel(c resolver.Candidate) string {
	switch {
	case c.Version != "":
		return c.Version
	case c.Tag != "":
		return c.Tag
	case c.Commit != "":
		if len(c.Commit) > 12 {
			return c.Commit[:12]
		}
		return c.Commit
	default:
		return ""
	}
}

// labelOr returns the repository label when set, else a fallback source label.
func labelOr(label, fallback string) string {
	if label == "" {
		return fallback
	}
	return label
}
