package selfupdatetest_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	selfupdatetest.Cleanup()
	os.Exit(code)
}

func TestBuildFake_ReportsVersion(t *testing.T) {
	t.Parallel()

	fake := selfupdatetest.BuildFake(t, "1.2.3", "ok")
	out, err := exec.CommandContext(context.Background(), fake, "version", "--json").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"version":"1.2.3"`) {
		t.Errorf("output = %q, want version 1.2.3", out)
	}
}

func TestArchive_RoundTrips(t *testing.T) {
	t.Parallel()

	data := selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Body: []byte("bin")})
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(tr)
	if hdr.Name != "gskill" || string(body) != "bin" {
		t.Errorf("entry = %q %q, want gskill bin", hdr.Name, body)
	}
}

func TestServer_ServesLatestAndAssets(t *testing.T) {
	t.Parallel()

	srv := selfupdatetest.NewServer(t, selfupdatetest.ServerOptions{Releases: []selfupdatetest.ReleaseSpec{
		{Tag: "v0.9.0", Assets: map[string][]byte{"a": []byte("old")}},
		{Tag: "v0.10.0", Assets: map[string][]byte{"a": []byte("new")}},
		{Tag: "v0.11.0-rc.1", Prerelease: true},
	}})
	resp, err := get(t, srv, srv.APIBase()+"/releases/latest")
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(meta), `"tag_name":"v0.10.0"`) {
		t.Errorf("latest = %s, want v0.10.0", meta)
	}
	resp, err = get(t, srv, srv.URL+"/download/v0.10.0/a")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "new" {
		t.Errorf("asset = %d %q, want 200 new", resp.StatusCode, body)
	}
	if !srv.AssetRequested("a") || !srv.AnyAssetRequested() {
		t.Error("asset request was not recorded")
	}
}

func get(t *testing.T, srv *selfupdatetest.Server, u string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Client().Do(req)
}

func TestVerifierAndEnv(t *testing.T) {
	t.Parallel()

	if err := (selfupdatetest.Verifier{Err: selfupdatetest.ErrFake}).VerifyManifest(context.Background(), nil, nil, "v1"); err == nil {
		t.Error("Verifier with Err returned nil")
	}
	env := selfupdatetest.Env(t, "/x/gskill", selfupdate.BuildInfo{Version: "0.9.0"}, nil, selfupdatetest.Verifier{})
	if exe, _ := env.Executable(); exe != "/x/gskill" {
		t.Errorf("Executable = %q", exe)
	}
	if _, err := env.LookPath("gskill"); err == nil {
		t.Error("LookPath found a gskill; the test env must not see the machine's PATH")
	}
	if got := selfupdatetest.Checksums(map[string][]byte{"f": []byte("x")}); !strings.HasSuffix(string(got), "  f\n") {
		t.Errorf("Checksums = %q", got)
	}
}
