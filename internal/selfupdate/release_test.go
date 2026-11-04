package selfupdate_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

func newSource(srv *selfupdatetest.Server, token string) selfupdate.Source {
	return selfupdate.NewGitHubSource(srv.Client(), srv.APIBase(), token)
}

func TestGitHubSource_Latest(t *testing.T) {
	t.Parallel()

	srv := selfupdatetest.NewServer(t, selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{
		{Tag: "v0.10.0", Assets: map[string][]byte{"checksums.txt": []byte("x")}},
	}})
	rel, err := newSource(srv, "").Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version.String() != "0.10.0" || rel.Tag != "v0.10.0" {
		t.Errorf("release = %s %s, want 0.10.0 v0.10.0", rel.Version, rel.Tag)
	}
	if a, ok := rel.Asset("checksums.txt"); !ok || a.Size != 1 || !strings.HasPrefix(a.URL, "https://") {
		t.Errorf("asset = %+v, %v", a, ok)
	}
}

func TestGitHubSource_ByTagNotPublished(t *testing.T) {
	t.Parallel()

	srv := selfupdatetest.NewServer(t, selfupdatetest.ServerOptions{})
	_, err := newSource(srv, "").ByTag(context.Background(), "v0.42.0")
	if err == nil || err.Error() != "v0.42.0 is not a published gskill release" {
		t.Fatalf("err = %v, want not published", err)
	}
	if got := errs.ExitCode(err); got != 2 {
		t.Errorf("ExitCode = %d, want 2", got)
	}
	if hint := errs.HintOf(err); hint != "see https://github.com/glapsfun/gskill/releases" {
		t.Errorf("hint = %q", hint)
	}
}

func TestGitHubSource_RefusesBadMetadata(t *testing.T) {
	t.Parallel()

	base := "/repos/glapsfun/gskill/releases/tags/"
	srv := selfupdatetest.NewServer(t, selfupdatetest.ServerOptions{
		Releases: []selfupdatetest.ReleaseSpec{{Tag: "v1.0.0", Draft: true}},
		Override: map[string]string{
			base + "v2.0.0": `{"tag_name":"latest-build","assets":[]}`,
			base + "v3.0.0": `{"tag_name":"v3.0.0","assets":[{"name":"checksums.txt","browser_download_url":"http://example.com/c","size":1}]}`,
		},
		Status: map[string]int{base + "v4.0.0": 500},
	})
	src := newSource(srv, "")
	for _, tag := range []string{"v1.0.0", "v2.0.0", "v3.0.0", "v4.0.0"} {
		if _, err := src.ByTag(context.Background(), tag); err == nil {
			t.Errorf("ByTag(%s) = nil error, want a refusal", tag)
		}
	}
	if _, err := src.ByTag(context.Background(), "v4.0.0"); errs.ExitCode(err) != 5 {
		t.Errorf("500 ExitCode = %d, want 5", errs.ExitCode(err))
	}
}

func TestGitHubSource_TokenOnlyForAPI(t *testing.T) {
	t.Parallel()

	srv := selfupdatetest.NewServer(t, selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{
		{Tag: "v0.10.0", Assets: map[string][]byte{"checksums.txt": []byte("x")}},
	}})
	src := newSource(srv, "secret")
	rel, err := src.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := rel.Asset("checksums.txt")
	rc, err := src.Open(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()
	if got := srv.Authorization("/repos/glapsfun/gskill/releases/latest"); got != "Bearer secret" {
		t.Errorf("API Authorization = %q, want Bearer secret", got)
	}
	if got := srv.Authorization("/download/v0.10.0/checksums.txt"); got != "" {
		t.Errorf("asset Authorization = %q, want none", got)
	}
}

func TestGitHubSource_OpenEnforcesSizes(t *testing.T) {
	t.Parallel()

	srv := selfupdatetest.NewServer(t, selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{
		{Tag: "v0.10.0", Assets: map[string][]byte{"checksums.txt": []byte("0123456789")}},
	}})
	src := newSource(srv, "")
	rel, err := src.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := rel.Asset("checksums.txt")

	// The release claims fewer bytes than the server sends.
	short := a
	short.Size = 4
	rc, err := src.Open(context.Background(), short)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(rc)
	_ = rc.Close()
	if err == nil || errs.ExitCode(err) != 6 {
		t.Errorf("oversized body: err = %v, want exit 6", err)
	}

	big := a
	big.Size = 2 << 20
	if _, err := src.Open(context.Background(), big); err == nil || errs.ExitCode(err) != 6 {
		t.Errorf("checksums.txt over its 1 MiB cap: err = %v, want exit 6", err)
	}
	archive := selfupdate.Asset{Name: "gskill_1_linux_amd64.tar.gz", URL: a.URL, Size: 129 << 20}
	if _, err := src.Open(context.Background(), archive); err == nil || errs.ExitCode(err) != 6 {
		t.Errorf("archive over its 128 MiB cap: err = %v, want exit 6", err)
	}
}
