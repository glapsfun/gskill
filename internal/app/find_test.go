package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/registry"
	"github.com/glapsfun/gskill/internal/source"
)

type fakeLister struct {
	repos    []registry.RepoRef
	warnings []string
	err      error
	// got records the scope the search asked for.
	got *registry.Scope
}

func (f fakeLister) ListRepos(_ context.Context, scope registry.Scope) (registry.Listing, error) {
	if f.got != nil {
		*f.got = scope
	}
	return registry.Listing{Repos: f.repos, Warnings: f.warnings}, f.err
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
	lister := fakeLister{repos: []registry.RepoRef{{FullPath: "glapsfun/skills", HTTPSURL: src, DefaultBranch: "main"}}}
	hits, _, err := findApp(t, nil, lister).Find(context.Background(), "code-review", app.FindScope{Owner: "glapsfun"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "code-review" {
		t.Errorf("owner-scope hits = %+v, want code-review", hits)
	}
}

// A host-qualified scope reaches the lister parsed, the chosen transport picks
// the clone address scanned, and hits name their provider and repository
// (spec 035 FR-022, FR-029, FR-030).
func TestFind_ProviderScopeAndTransport(t *testing.T) {
	t.Parallel()
	https := sourceTree(t, "skills/code-review")
	ssh := sourceTree(t, "skills/lint")
	var got registry.Scope
	lister := fakeLister{
		repos:    []registry.RepoRef{{Provider: source.ProviderGitLab, FullPath: "acme/platform/skills", HTTPSURL: https, SSHURL: ssh}},
		warnings: []string{"listing public repositories only; set GITLAB_TOKEN to include private ones"},
		got:      &got,
	}
	hits, warnings, err := findApp(t, nil, lister).Find(context.Background(), "",
		app.FindScope{Owner: "gitlab.com/acme/platform", Transport: "ssh"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Provider != source.ProviderGitLab || got.Path != "acme/platform" || got.Transport != registry.TransportSSH {
		t.Errorf("lister got scope %+v, want gitlab acme/platform over ssh", got)
	}
	if len(hits) != 1 || hits[0].ID != "lint" || hits[0].Source != ssh {
		t.Errorf("hits = %+v, want lint from the SSH address", hits)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want the lister's public-only warning", warnings)
	}
}

func TestFind_HitsNameProviderAndRepository(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/code-review")
	hits, _, err := findApp(t, nil, nil).Find(context.Background(), "", app.FindScope{Source: src})
	if err != nil || len(hits) != 1 {
		t.Fatalf("Find = %+v, %v", hits, err)
	}
	if hits[0].Provider != "local" || hits[0].Repository == "" {
		t.Errorf("hit provider %q repository %q, want local and the directory", hits[0].Provider, hits[0].Repository)
	}
}

// A scope that cannot be listed fails the search with the lister's error
// instead of reporting no matches.
func TestFind_ListingFailureIsFatal(t *testing.T) {
	t.Parallel()
	denied := errs.New(errs.CodeAuth, "GitLab rejected the token in GITLAB_TOKEN (expired or invalid)")
	_, _, err := findApp(t, nil, fakeLister{err: denied}).Find(context.Background(), "x", app.FindScope{Owner: "gitlab.com/acme"})
	if errs.ExitCode(err) != int(errs.CodeAuth) {
		t.Errorf("Find error = %v (exit %d), want the lister's auth failure", err, errs.ExitCode(err))
	}
}

func TestFind_UnsupportedScopeHost(t *testing.T) {
	t.Parallel()
	_, _, err := findApp(t, nil, fakeLister{}).Find(context.Background(), "x", app.FindScope{Owner: "git.example.com/acme"})
	if errs.ExitCode(err) != int(errs.CodeUsage) {
		t.Errorf("Find error = %v, want a usage error naming the supported providers", err)
	}
}
