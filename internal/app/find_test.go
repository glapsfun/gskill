package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/registry"
)

type fakeLister struct {
	repos []registry.RepoRef
	err   error
}

func (f fakeLister) ListOwnerRepos(_ context.Context, _ string) ([]registry.RepoRef, error) {
	return f.repos, f.err
}

func findApp(t *testing.T, repos []string, lister app.RepoLister) *app.App {
	t.Helper()
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: &config.Config{Repositories: repos},
		Repos:  lister,
	})
}

func TestFind_SourceScope(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/kubernetes-ops", "skills/writing")
	hits, _, err := findApp(t, nil, nil).Find(context.Background(), "kubernetes", app.FindScope{Source: src})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "kubernetes-ops" {
		t.Errorf("hits = %+v, want kubernetes-ops", hits)
	}
	if hits[0].Source != src || hits[0].RepoPath != "skills/kubernetes-ops" {
		t.Errorf("hit not attributed to source+path: %+v", hits[0])
	}
}

func TestFind_ConfiguredRepos(t *testing.T) {
	t.Parallel()
	a := sourceTree(t, "skills/code-review")
	b := sourceTree(t, "skills/code-review-helper")
	hits, _, err := findApp(t, []string{a, b}, nil).Find(context.Background(), "code-review", app.FindScope{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("got %d hits across configured repos, want 2: %+v", len(hits), hits)
	}
	// Exact id match ranks above substring.
	if hits[0].ID != "code-review" {
		t.Errorf("ranking: first hit = %q, want code-review (exact)", hits[0].ID)
	}
}

func TestFind_UnreachableRepoWarnsNotFatal(t *testing.T) {
	t.Parallel()
	good := sourceTree(t, "skills/code-review")
	hits, warnings, err := findApp(t, []string{good, "/nonexistent/repo/path"}, nil).
		Find(context.Background(), "code-review", app.FindScope{})
	if err != nil {
		t.Fatalf("Find should not fail on one bad repo: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("got %d hits, want 1 from the reachable repo", len(hits))
	}
	if len(warnings) == 0 {
		t.Error("expected a warning for the unreachable repo")
	}
}

func TestFind_OwnerScope(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/code-review")
	lister := fakeLister{repos: []registry.RepoRef{{Name: "skills", CloneURL: src, DefaultBranch: "main"}}}
	hits, _, err := findApp(t, nil, lister).Find(context.Background(), "code-review", app.FindScope{Owner: "glapsfun"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "code-review" {
		t.Errorf("owner-scope hits = %+v, want code-review", hits)
	}
}
