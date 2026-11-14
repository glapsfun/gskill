package registry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/registry"
	"github.com/glapsfun/gskill/internal/source"
)

// plantedToken is the discovery token the fakes expect. It must never appear
// in an error, a warning, or a repository field.
const plantedToken = "tok-SECRET-035" // gitleaks:allow (a fake value the leak checks search for)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// recorder notes the Authorization header of every request a fake serves.
type recorder struct {
	mu    sync.Mutex
	auths []string
	paths []string
}

func (r *recorder) note(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.auths = append(r.auths, req.Header.Get("Authorization"))
	r.paths = append(r.paths, req.URL.RequestURI())
}

func (r *recorder) sawToken() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.auths {
		if strings.Contains(a, plantedToken) {
			return true
		}
	}
	return false
}

func envWith(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ghRepos builds n GitHub repository objects named <owner>/r<start..>.
func ghRepos(owner string, start, n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := start; i < start+n; i++ {
		full := fmt.Sprintf("%s/r%d", owner, i)
		if i == 1 {
			// Repository names may start with a dot, such as .github.
			full = owner + "/.github"
		}
		out = append(out, map[string]any{
			"full_name": full, "clone_url": "https://github.com/" + full + ".git",
			"ssh_url": "git@github.com:" + full + ".git", "private": i%2 == 0, "default_branch": "main",
		})
	}
	return out
}

// githubFake serves an organization whose repositories span pages of the
// given sizes, linked with rel="next".
func githubFake(t *testing.T, rec *recorder, sizes []int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.note(r)
		switch r.URL.Path {
		case "/users/acme":
			writeJSON(w, map[string]string{"type": "Organization", "login": "acme"})
		case "/orgs/acme/repos":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page == 0 {
				page = 1
			}
			start := 0
			for _, s := range sizes[:page-1] {
				start += s
			}
			if page < len(sizes) {
				w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/acme/repos?type=all&per_page=100&page=%d>; rel="next", <%s/x>; rel="last"`, srv.URL, page+1, srv.URL))
			}
			writeJSON(w, ghRepos("acme", start, sizes[page-1]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func gh(srv *httptest.Server) map[source.Provider]string {
	return map[source.Provider]string{source.ProviderGitHub: srv.URL}
}

func list(t *testing.T, bases map[source.Provider]string, env map[string]string, scope string) (registry.Listing, error) {
	t.Helper()
	s, err := registry.ParseScope(scope)
	if err != nil {
		t.Fatalf("ParseScope(%q): %v", scope, err)
	}
	return registry.NewListerWithBases(bases, envWith(env)).ListRepos(context.Background(), s)
}

func TestLister_FollowsLinkPages(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := githubFake(t, rec, []int{100, 100, 37})
	got, err := list(t, gh(srv), map[string]string{"GITHUB_TOKEN": plantedToken}, "acme")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got.Repos) != 237 || got.Repos[236].FullPath != "acme/r236" || !got.Authenticated {
		t.Fatalf("listed %d repos (last %+v), want 237 ending at acme/r236", len(got.Repos), got.Repos[len(got.Repos)-1])
	}
	for _, a := range rec.auths {
		if a != "Bearer "+plantedToken {
			t.Errorf("request carried Authorization %q, want the token on every API request", a)
		}
	}
	assertNoToken(t, got, nil)
}

func TestLister_StopsAtTheCap(t *testing.T) {
	t.Parallel()

	sizes := make([]int, 12)
	for i := range sizes {
		sizes[i] = 100
	}
	got, err := list(t, gh(githubFake(t, &recorder{}, sizes)), nil, "acme")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got.Repos) != 1000 || !hasWarning(got, "listing stopped at 1000 repositories") {
		t.Errorf("listed %d repos with warnings %v, want 1000 and the cut-off warning", len(got.Repos), got.Warnings)
	}
}

func TestLister_IgnoresForeignNextLink(t *testing.T) {
	t.Parallel()

	foreign := &recorder{}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreign.note(r)
		writeJSON(w, ghRepos("evil", 0, 1))
	}))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/acme":
			writeJSON(w, map[string]string{"type": "Organization"})
		default:
			w.Header().Set("Link", `<`+other.URL+`/orgs/acme/repos?page=2>; rel="next"`)
			writeJSON(w, ghRepos("acme", 0, 2))
		}
	}))
	t.Cleanup(srv.Close)

	got, err := list(t, gh(srv), map[string]string{"GITHUB_TOKEN": plantedToken}, "acme")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got.Repos) != 2 || !hasWarning(got, "ignored a next-page link to") {
		t.Errorf("got %d repos, warnings %v; want 2 and the ignored-link warning", len(got.Repos), got.Warnings)
	}
	if len(foreign.paths) != 0 {
		t.Errorf("the other origin was contacted: %v", foreign.paths)
	}
}

// A redirect to another origin (here the same host on another port) is
// refused, and the token never reaches it (spec 035 FR-027).
func TestLister_RefusesCrossOriginRedirect(t *testing.T) {
	t.Parallel()

	foreign := &recorder{}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreign.note(r)
		writeJSON(w, map[string]string{"type": "Organization"})
	}))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound) //nolint:gosec // the redirect to another origin is what the test checks
	}))
	t.Cleanup(srv.Close)

	_, err := list(t, gh(srv), map[string]string{"GITHUB_TOKEN": plantedToken}, "acme")
	if err == nil {
		t.Fatal("ListRepos followed a cross-origin redirect")
	}
	if foreign.sawToken() || len(foreign.paths) != 0 {
		t.Errorf("the redirect target was contacted (%v)", foreign.paths)
	}
	if !contains(err.Error(), "redirected to another origin") {
		t.Errorf("error %q does not say the redirect was refused", err)
	}
	if contains(err.Error(), plantedToken) {
		t.Errorf("error %q contains the token", err)
	}
}

func TestLister_OversizedResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/acme" {
			writeJSON(w, map[string]string{"type": "Organization"})
			return
		}
		_, _ = w.Write([]byte("[" + strings.Repeat(" ", 9<<20) + "]"))
	}))
	t.Cleanup(srv.Close)
	if _, err := list(t, gh(srv), nil, "acme"); err == nil {
		t.Error("ListRepos accepted a response over the 8 MiB limit")
	}
}

func TestLister_Cancelled(t *testing.T) {
	t.Parallel()

	srv := githubFake(t, &recorder{}, []int{1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, _ := registry.ParseScope("acme")
	_, err := registry.NewListerWithBases(gh(srv), envWith(nil)).ListRepos(ctx, s)
	if errs.ExitCode(err) != int(errs.CodeCancelled) {
		t.Errorf("cancelled ListRepos = %v (exit %d), want the cancellation code", err, errs.ExitCode(err))
	}
}

func TestLister_Unreachable(t *testing.T) {
	t.Parallel()

	srv := githubFake(t, &recorder{}, []int{1})
	srv.Close()
	_, err := list(t, gh(srv), nil, "acme")
	if errs.ExitCode(err) != int(errs.CodeSourceUnavailable) || !contains(err.Error(), "cannot reach") {
		t.Errorf("ListRepos against a closed server = %v, want cannot reach (exit 5)", err)
	}
}

func hasWarning(l registry.Listing, sub string) bool {
	for _, w := range l.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// assertNoToken checks a listing and an error for the planted token.
func assertNoToken(t *testing.T, l registry.Listing, err error) {
	t.Helper()
	if err != nil && contains(err.Error(), plantedToken) {
		t.Errorf("error contains the token: %v", err)
	}
	for _, w := range l.Warnings {
		if contains(w, plantedToken) {
			t.Errorf("warning contains the token: %s", w)
		}
	}
	for _, r := range l.Repos {
		if contains(fmt.Sprintf("%+v", r), plantedToken) {
			t.Errorf("repository contains the token: %+v", r)
		}
	}
}
