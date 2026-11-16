package registry_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/registry"
	"github.com/glapsfun/gskill/internal/source"
)

// GitHub picks the endpoint from the owner's type and whether the owner is
// the token's own account; a token that cannot read /user falls through to
// the public endpoint (spec 035 research R8).
func TestGitHub_Endpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ownerTyp string
		meStatus int
		env      map[string]string
		wantPath string
	}{
		{"organization", "Organization", http.StatusOK, map[string]string{"GITHUB_TOKEN": plantedToken}, "/orgs/acme/repos?type=all&per_page=100"},
		{"own account", "User", http.StatusOK, map[string]string{"GH_TOKEN": plantedToken}, "/user/repos?affiliation=owner&per_page=100"},
		{"installation token", "User", http.StatusForbidden, map[string]string{"GITHUB_TOKEN": plantedToken}, "/users/acme/repos?per_page=100"},
		{"other user without a token", "User", http.StatusOK, nil, "/users/acme/repos?per_page=100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.note(r)
				if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
					t.Errorf("Accept = %q, want GitHub's media type", got)
				}
				switch r.URL.Path {
				case "/users/acme":
					writeJSON(w, map[string]string{"type": tt.ownerTyp})
				case "/user":
					if tt.meStatus != http.StatusOK {
						http.Error(w, "forbidden", tt.meStatus)
						return
					}
					writeJSON(w, map[string]string{"login": "Acme"})
				default:
					writeJSON(w, ghRepos("acme", 0, 3))
				}
			}))
			t.Cleanup(srv.Close)

			got, err := list(t, gh(srv), tt.env, "github.com/acme")
			if err != nil {
				t.Fatalf("ListRepos: %v", err)
			}
			if last := rec.paths[len(rec.paths)-1]; last != tt.wantPath {
				t.Errorf("listed with %s, want %s", last, tt.wantPath)
			}
			r := got.Repos[0]
			if r.HTTPSURL != "https://github.com/acme/r0.git" || r.SSHURL != "git@github.com:acme/r0.git" || r.Provider != source.ProviderGitHub {
				t.Errorf("repo = %+v, want built github.com clone addresses", r)
			}
			if tt.env == nil && !hasWarning(got, "set GITHUB_TOKEN to include private ones") {
				t.Errorf("warnings %v lack the public-only note", got.Warnings)
			}
			assertNoToken(t, got, nil)
		})
	}
}

func TestGitHub_StatusMapping(t *testing.T) {
	t.Parallel()

	reset := strconv.FormatInt(time.Date(2026, 11, 14, 18, 0, 0, 0, time.UTC).Unix(), 10)
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		env     map[string]string
		code    errs.Code
		want    string
	}{
		{"expired token", 401, nil, map[string]string{"GITHUB_TOKEN": plantedToken}, errs.CodeAuth, "GitHub rejected the token in GITHUB_TOKEN (expired or invalid)"},
		{"sso", 403, map[string]string{"X-GitHub-SSO": "required; url=https://github.com/orgs/acme/sso"}, map[string]string{"GITHUB_TOKEN": plantedToken}, errs.CodeAuth, "authorized for single sign-on"},
		{"rate limit", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": reset}, nil, errs.CodeSourceUnavailable, "GitHub rate limit reached; resets at 2026-11-14T18:00:00Z; set GITHUB_TOKEN to raise the limit"},
		{"too many requests", 429, map[string]string{"Retry-After": "30"}, map[string]string{"GITHUB_TOKEN": plantedToken}, errs.CodeSourceUnavailable, "GitHub rate limit reached; retry after 30s"},
		{"forbidden", 403, nil, map[string]string{"GITHUB_TOKEN": plantedToken}, errs.CodeAuth, "the token in GITHUB_TOKEN is not allowed to list github.com/acme"},
		{"not visible", 404, nil, nil, errs.CodeSourceUnavailable, "github.com/acme not found or not accessible with the current credentials"},
		{"server error", 502, nil, nil, errs.CodeSourceUnavailable, "GitHub returned 502"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				http.Error(w, "nope", tt.status)
			}))
			t.Cleanup(srv.Close)
			got, err := list(t, gh(srv), tt.env, "acme")
			if errs.ExitCode(err) != int(tt.code) || !contains(fmt.Sprint(err), tt.want) {
				t.Errorf("err = %v (exit %d), want exit %d containing %q", err, errs.ExitCode(err), tt.code, tt.want)
			}
			if contains(fmt.Sprint(err), "does not exist") {
				t.Errorf("err %v claims the scope does not exist", err)
			}
			assertNoToken(t, got, err)
		})
	}
}

// gitlabProject builds a GitLab project object.
func gitlabProject(path, visibility string) map[string]any {
	return map[string]any{
		"path_with_namespace": path, "http_url_to_repo": "https://gitlab.com/" + path + ".git",
		"ssh_url_to_repo": "git@gitlab.com:" + path + ".git", "visibility": visibility, "default_branch": "main",
	}
}

func TestGitLab_GroupWithSubgroupsAcrossPages(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.note(r)
		if r.URL.EscapedPath() != "/groups/acme%2Fplatform/projects" || r.URL.Query().Get("include_subgroups") != "true" {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 3 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/groups/acme%%2Fplatform/projects?include_subgroups=true&per_page=100&page=%d>; rel="next"`, srv.URL, max(page, 1)+1))
		}
		writeJSON(w, []map[string]any{
			gitlabProject(fmt.Sprintf("acme/platform/p%d", page), "private"),
			gitlabProject(fmt.Sprintf("acme/platform/sub/q%d", page), "public"),
			{"path_with_namespace": "acme/platform/evil", "http_url_to_repo": "https://evil.example/x.git", "ssh_url_to_repo": "git@evil.example:x.git"},
		})
	}))
	t.Cleanup(srv.Close)

	got, err := list(t, map[source.Provider]string{source.ProviderGitLab: srv.URL}, map[string]string{"GITLAB_TOKEN": plantedToken}, "gitlab.com/acme/platform")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got.Repos) != 6 {
		t.Fatalf("listed %d repos over 3 pages, want 6: %+v", len(got.Repos), got.Repos)
	}
	nested := got.Repos[1]
	if nested.FullPath != "acme/platform/sub/q0" || nested.SSHURL != "git@gitlab.com:acme/platform/sub/q0.git" || nested.Private {
		t.Errorf("subgroup project = %+v", nested)
	}
	if !got.Repos[0].Private || !hasWarning(got, "skipped acme/platform/evil") {
		t.Errorf("private flag or foreign-host warning missing: %+v %v", got.Repos[0], got.Warnings)
	}
	if !rec.sawToken() {
		t.Error("GITLAB_TOKEN was not sent")
	}
	assertNoToken(t, got, nil)
}

func TestGitLab_UserFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/jane/projects" {
			writeJSON(w, []map[string]any{gitlabProject("jane/skills", "public")})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	got, err := list(t, map[source.Provider]string{source.ProviderGitLab: srv.URL}, nil, "gitlab.com/jane")
	if err != nil || len(got.Repos) != 1 || got.Repos[0].FullPath != "jane/skills" {
		t.Fatalf("user fallback = %+v, %v", got, err)
	}
	_, err = list(t, map[source.Provider]string{source.ProviderGitLab: srv.URL}, nil, "gitlab.com/acme/missing")
	if errs.ExitCode(err) != int(errs.CodeSourceUnavailable) || !contains(err.Error(), "gitlab.com/acme/missing not found or not accessible") {
		t.Errorf("nested missing group = %v, want not found or not accessible", err)
	}
}

func TestBitbucket_WorkspaceAcrossPages(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.note(r)
		if r.URL.Path != "/repositories/acme" {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		page = max(page, 1)
		body := map[string]any{"values": []map[string]any{{
			"full_name": fmt.Sprintf("acme/r%d", page), "is_private": true, "mainbranch": map[string]string{"name": "main"},
			"links": map[string]any{"clone": []map[string]string{
				{"name": "https", "href": fmt.Sprintf("https://jane@bitbucket.org/acme/r%d.git", page)},
				{"name": "ssh", "href": fmt.Sprintf("git@ssh.bitbucket.org:acme/r%d.git", page)},
			}},
		}}}
		if page < 3 {
			body["next"] = fmt.Sprintf("%s/repositories/acme?pagelen=100&page=%d", srv.URL, page+1)
		}
		writeJSON(w, body)
	}))
	t.Cleanup(srv.Close)

	bases := map[source.Provider]string{source.ProviderBitbucket: srv.URL}
	got, err := list(t, bases, map[string]string{"BITBUCKET_TOKEN": plantedToken, "BITBUCKET_EMAIL": "jane@example.com"}, "bitbucket.org/acme")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	// The reported SSH host is Bitbucket's own, so its address is used as is.
	if len(got.Repos) != 3 || got.Repos[2].HTTPSURL != "https://bitbucket.org/acme/r3.git" ||
		got.Repos[2].SSHURL != "git@ssh.bitbucket.org:acme/r3.git" || !got.Repos[0].Private {
		t.Fatalf("listed %+v, want acme/r1..r3 with built clone addresses", got.Repos)
	}
	if rec.auths[0] == "" || contains(rec.auths[0], "Bearer") {
		t.Errorf("with BITBUCKET_EMAIL set, auth = %q, want Basic", rec.auths[0])
	}
	assertNoToken(t, got, nil)

	bearerRec := &recorder{}
	bearer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearerRec.note(r)
		writeJSON(w, map[string]any{"values": []any{}})
	}))
	t.Cleanup(bearer.Close)
	got, err = list(t, map[source.Provider]string{source.ProviderBitbucket: bearer.URL}, map[string]string{"BITBUCKET_TOKEN": plantedToken}, "bitbucket.org/empty")
	if err != nil || bearerRec.auths[0] != "Bearer "+plantedToken {
		t.Errorf("bearer auth = %q, %v", bearerRec.auths, err)
	}
	if !hasWarning(got, "no repositories are visible in bitbucket.org/empty") {
		t.Errorf("empty workspace warnings = %v", got.Warnings)
	}
}

func TestLister_UnknownProvider(t *testing.T) {
	t.Parallel()

	_, err := registry.NewListerWithBases(nil, envWith(nil)).ListRepos(t.Context(), registry.Scope{Provider: source.ProviderGitHub, Path: "acme"})
	if errs.ExitCode(err) != int(errs.CodeUsage) {
		t.Errorf("ListRepos with no base = %v, want a usage error", err)
	}
}
