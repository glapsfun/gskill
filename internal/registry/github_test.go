package registry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/registry"
)

func TestListOwnerRepos_User(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/users/glapsfun/repos") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"name":"skills","clone_url":"https://github.com/glapsfun/skills.git","default_branch":"main"}]`))
	}))
	defer srv.Close()

	repos, err := registry.NewWithBase(srv.URL).ListOwnerRepos(context.Background(), "glapsfun")
	if err != nil {
		t.Fatalf("ListOwnerRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "skills" {
		t.Errorf("repos = %+v, want one named skills", repos)
	}
	if repos[0].DefaultBranch != "main" {
		t.Errorf("default branch = %q", repos[0].DefaultBranch)
	}
}

func TestListOwnerRepos_OrgFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/users/"):
			http.Error(w, "not a user", http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/orgs/acme/repos"):
			_, _ = w.Write([]byte(`[{"name":"agent-skills","clone_url":"https://github.com/acme/agent-skills.git","default_branch":"trunk"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	repos, err := registry.NewWithBase(srv.URL).ListOwnerRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListOwnerRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "agent-skills" {
		t.Errorf("repos = %+v, want org fallback result", repos)
	}
}

func TestListOwnerRepos_Unreachable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := registry.NewWithBase(srv.URL).ListOwnerRepos(context.Background(), "x"); err == nil {
		t.Error("expected an error for an unreachable/erroring API")
	}
}
