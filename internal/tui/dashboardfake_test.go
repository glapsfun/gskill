package tui

import (
	"context"
	"errors"

	"github.com/glapsfun/gskill/internal/source"
)

// fakeService is a scriptable DashboardService for model tests. Inventory and
// each action return canned values or errors so a test can drive the model
// through its states without a real project.
type fakeService struct {
	skills    []DashboardSkill
	invErr    error
	versions  []string
	applyOut  []ActionOutcome
	applyErr  error
	plan      ProposedChange
	planErr   error
	removed   map[string]bool
	applyName string // last name an apply was called with
}

func (f *fakeService) Inventory(context.Context) ([]DashboardSkill, error) {
	if f.invErr != nil {
		return nil, f.invErr
	}
	// Return skills minus any removed this session (simulates a refresh).
	out := make([]DashboardSkill, 0, len(f.skills))
	for _, s := range f.skills {
		if f.removed[s.Name] {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeService) Markdown(_ context.Context, name string) (string, error) {
	return "# " + name + "\n", nil
}

func (f *fakeService) Versions(context.Context, string) ([]string, error) {
	return f.versions, nil
}

func (f *fakeService) PlanUpdate(_ context.Context, name string) (ProposedChange, error) {
	return f.planFor(ChangeUpdate, name)
}

func (f *fakeService) PlanVersionChange(_ context.Context, name, _ string, _ bool) (ProposedChange, error) {
	return f.planFor(ChangeVersion, name)
}

func (f *fakeService) PlanRemove(_ context.Context, name string) (ProposedChange, error) {
	return f.planFor(ChangeRemove, name)
}

func (f *fakeService) planFor(kind ChangeKind, name string) (ProposedChange, error) {
	if f.planErr != nil {
		return ProposedChange{}, f.planErr
	}
	p := f.plan
	p.Kind, p.Target = kind, name
	return p, nil
}

func (f *fakeService) ApplyUpdate(ctx context.Context, name string) ([]ActionOutcome, error) {
	return f.apply(ctx, name)
}

func (f *fakeService) ApplyVersionChange(ctx context.Context, name, _ string, _ bool) ([]ActionOutcome, error) {
	return f.apply(ctx, name)
}

func (f *fakeService) ApplyRemove(ctx context.Context, name string) ([]ActionOutcome, error) {
	out, err := f.apply(ctx, name)
	if err == nil {
		if f.removed == nil {
			f.removed = map[string]bool{}
		}
		f.removed[name] = true
	}
	return out, err
}

func (f *fakeService) ApplyGroupUpdate(ctx context.Context, repoKey string) ([]ActionOutcome, error) {
	return f.apply(ctx, repoKey)
}

func (f *fakeService) apply(_ context.Context, name string) ([]ActionOutcome, error) {
	f.applyName = name
	if f.applyErr != nil {
		return f.applyOut, f.applyErr
	}
	return f.applyOut, nil
}

// dashSkills returns a two-repository inventory fixture for model tests.
func dashSkills() []DashboardSkill {
	return []DashboardSkill{
		{
			Name: "tui-design", InstalledVersion: "1.0.1", SourceLabel: "acme/skills",
			Repo:   source.Repo{Kind: source.RepoRemote, Key: "github.com/acme/skills", Label: "acme/skills"},
			Health: "installed", UpdateStatus: "up-to-date", Policy: "^1.0.0",
			Agents: []string{"claude"}, Markdown: "# tui-design\n\nGuidance.",
		},
		{
			Name: "deploy", InstalledVersion: "0.9.2", SourceLabel: "corp/devops", Candidate: "0.10.0",
			Repo:   source.Repo{Kind: source.RepoRemote, Key: "github.com/corp/devops", Label: "corp/devops"},
			Health: "outdated", UpdateStatus: "update-available", Policy: "^0.9.0",
			Agents: []string{"claude", "codex"}, Markdown: "# deploy\n\nHelpers.",
		},
	}
}

var errFake = errors.New("fake failure")
