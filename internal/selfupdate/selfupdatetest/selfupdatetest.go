// Package selfupdatetest holds test helpers for the self-upgrade code: a fake
// GitHub release server, a fake signature verifier, an in-memory archive
// builder, and a small fake gskill binary built on demand.
package selfupdatetest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/selfupdate"
)

var (
	fakeMu    sync.Mutex
	fakeDir   string
	fakeBuilt = map[string]string{}
)

// Cleanup removes the binaries BuildFake cached. Call it from TestMain after
// m.Run; without it the cache stays in the system temp directory.
func Cleanup() {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	if fakeDir != "" {
		_ = os.RemoveAll(fakeDir)
		fakeDir = ""
		fakeBuilt = map[string]string{}
	}
}

// BuildFake returns a fake gskill for this platform that reports version and
// behaves according to mode ("ok", "badjson", "hang", or "fail"). Each
// (version, mode) pair is built once per test process.
func BuildFake(tb testing.TB, version, mode string) string {
	tb.Helper()
	return build(tb, runtime.GOOS, runtime.GOARCH, version, mode)
}

// BuildFakeFor cross-compiles the fake for another platform, for header checks.
func BuildFakeFor(tb testing.TB, goos, goarch, version string) string {
	tb.Helper()
	return build(tb, goos, goarch, version, "ok")
}

func build(tb testing.TB, goos, goarch, version, mode string) string {
	tb.Helper()
	fakeMu.Lock()
	defer fakeMu.Unlock()

	key := strings.Join([]string{goos, goarch, version, mode}, "_")
	if path, ok := fakeBuilt[key]; ok {
		return path
	}
	if fakeDir == "" {
		// Shared across tests on purpose: each fake is built once per process
		// and removed by Cleanup from TestMain.
		dir, err := os.MkdirTemp("", "gskill-fake-*") //nolint:usetesting // outlives any single test, see Cleanup
		if err != nil {
			tb.Fatalf("selfupdatetest: temp dir: %v", err)
		}
		fakeDir = dir
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("selfupdatetest: cannot locate the directory of the fake binary")
	}
	src := filepath.Join(filepath.Dir(file), "..", "testdata", "fakegskill")
	out := filepath.Join(fakeDir, key, "gskill")

	ldflags := "-X main.version=" + version + " -X main.mode=" + mode
	cmd := exec.CommandContext(context.Background(), "go", "build", "-trimpath", "-o", out, "-ldflags", ldflags, ".") //nolint:gosec // builds the fake from this package's testdata
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	if msg, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("selfupdatetest: build fake gskill %s: %v\n%s", key, err, msg)
	}
	fakeBuilt[key] = out
	return out
}

// Entry is one file in an archive built by Archive.
type Entry struct {
	Name string
	Body []byte
	Mode int64
	// Type defaults to tar.TypeReg.
	Type byte
	// Size overrides the header size, to describe oversized entries without
	// writing their bytes.
	Size int64
}

// Archive returns a tar.gz holding entries, in order.
func Archive(tb testing.TB, entries ...Entry) []byte {
	tb.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.Name, Mode: e.Mode, Typeflag: e.Type, Size: int64(len(e.Body))}
		if hdr.Mode == 0 {
			hdr.Mode = 0o755
		}
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if hdr.Typeflag != tar.TypeReg {
			hdr.Size = 0
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			hdr.Linkname = "elsewhere"
		}
		if e.Size != 0 {
			// The writer refuses to close a short entry, so an oversized
			// header ends the archive right after it.
			hdr.Size = e.Size
			if err := tw.WriteHeader(hdr); err != nil {
				tb.Fatalf("selfupdatetest: tar header: %v", err)
			}
			_ = tw.Flush()
			_ = gz.Close()
			return buf.Bytes()
		}
		if err := tw.WriteHeader(hdr); err != nil {
			tb.Fatalf("selfupdatetest: tar header: %v", err)
		}
		if _, err := tw.Write(e.Body); err != nil {
			tb.Fatalf("selfupdatetest: tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		tb.Fatalf("selfupdatetest: tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		tb.Fatalf("selfupdatetest: gzip close: %v", err)
	}
	return buf.Bytes()
}

// Checksums returns a checksums.txt body for the given name to content map.
func Checksums(files map[string][]byte) []byte {
	var b strings.Builder
	for name, body := range files {
		fmt.Fprintf(&b, "%s  %s\n", sha256Hex(body), name)
	}
	return []byte(b.String())
}

// ReleaseSpec is one release the fake server publishes.
type ReleaseSpec struct {
	Tag        string
	Prerelease bool
	Draft      bool
	Assets     map[string][]byte
}

// ServerOptions configure the fake GitHub server.
type ServerOptions struct {
	Releases []ReleaseSpec
	// Status forces a status code for a request path.
	Status map[string]int
	// RateLimited answers every API request with 403 and
	// X-RateLimit-Remaining: 0.
	RateLimited bool
	// DropAfter cuts the connection after this many bytes of the named asset.
	DropAfter map[string]int
	// Block holds the named asset's response until the channel is closed.
	Block map[string]chan struct{}
	// Override serves this raw body for a request path.
	Override map[string]string
}

// Server is a fake GitHub API and release download host over TLS.
type Server struct {
	*httptest.Server
	opts     ServerOptions
	mu       sync.Mutex
	requests []string
	auth     map[string]string
}

const repoPath = "/repos/glapsfun/gskill"

// NewServer starts a fake server that is closed when the test ends.
func NewServer(tb testing.TB, opts ServerOptions) *Server {
	tb.Helper()
	s := &Server{opts: opts, auth: map[string]string{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.handle))
	tb.Cleanup(s.Close)
	return s
}

// APIBase is the repository API base the GitHub source should use.
func (s *Server) APIBase() string { return s.URL + repoPath }

// Requests returns every request path the server has seen, in order.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// Authorization returns the Authorization header of the last request for path.
func (s *Server) Authorization(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth[path]
}

// AssetRequested reports whether any request downloaded the named asset.
func (s *Server) AssetRequested(name string) bool {
	for _, p := range s.Requests() {
		if strings.HasPrefix(p, "/download/") && strings.HasSuffix(p, "/"+name) {
			return true
		}
	}
	return false
}

// AnyAssetRequested reports whether any asset was downloaded.
func (s *Server) AnyAssetRequested() bool {
	for _, p := range s.Requests() {
		if strings.HasPrefix(p, "/download/") {
			return true
		}
	}
	return false
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.Path)
	s.auth[r.URL.Path] = r.Header.Get("Authorization")
	s.mu.Unlock()

	if code, ok := s.opts.Status[r.URL.Path]; ok {
		w.WriteHeader(code)
		return
	}
	if body, ok := s.opts.Override[r.URL.Path]; ok {
		_, _ = w.Write([]byte(body))
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, repoPath+"/releases"):
		s.serveMetadata(w, r)
	case strings.HasPrefix(r.URL.Path, "/download/"):
		s.serveAsset(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveMetadata(w http.ResponseWriter, r *http.Request) {
	if s.opts.RateLimited {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var rel *ReleaseSpec
	if r.URL.Path == repoPath+"/releases/latest" {
		for i := range s.opts.Releases {
			if !s.opts.Releases[i].Prerelease && !s.opts.Releases[i].Draft {
				rel = &s.opts.Releases[i]
			}
		}
	} else {
		tag := strings.TrimPrefix(r.URL.Path, repoPath+"/releases/tags/")
		for i := range s.opts.Releases {
			if s.opts.Releases[i].Tag == tag {
				rel = &s.opts.Releases[i]
			}
		}
	}
	if rel == nil {
		http.NotFound(w, r)
		return
	}
	type asset struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int    `json:"size"`
	}
	body := struct {
		TagName    string  `json:"tag_name"`
		Prerelease bool    `json:"prerelease"`
		Draft      bool    `json:"draft"`
		Assets     []asset `json:"assets"`
	}{TagName: rel.Tag, Prerelease: rel.Prerelease, Draft: rel.Draft, Assets: []asset{}}
	for name, data := range rel.Assets {
		body.Assets = append(body.Assets, asset{Name: name, URL: s.URL + "/download/" + rel.Tag + "/" + name, Size: len(data)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/download/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	var data []byte
	found := false
	for _, rel := range s.opts.Releases {
		if rel.Tag == parts[0] {
			data, found = rel.Assets[parts[1]]
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	if ch, ok := s.opts.Block[parts[1]]; ok {
		select {
		case <-ch:
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if n, ok := s.opts.DropAfter[parts[1]]; ok && n < len(data) {
		_, _ = w.Write(data[:n])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		panic(http.ErrAbortHandler)
	}
	_, _ = w.Write(data)
}

// Verifier is a selfupdate.Verifier that returns Err.
type Verifier struct{ Err error }

// VerifyManifest returns v.Err.
func (v Verifier) VerifyManifest(context.Context, []byte, []byte, string) error { return v.Err }

// Env returns an Env for exe with the given build, source, and verifier. PATH
// lookups find nothing, so a gskill installed on the test machine never
// shows up as a shadow executable.
func Env(tb testing.TB, exe string, build selfupdate.BuildInfo, src selfupdate.Source, v selfupdate.Verifier) selfupdate.Env {
	tb.Helper()
	return selfupdate.Env{
		Source:     src,
		Verifier:   v,
		Executable: func() (string, error) { return exe, nil },
		Args0:      exe,
		LookPath:   func(string) (string, error) { return "", exec.ErrNotFound },
		Getenv:     os.Getenv,
		ReadFile:   os.ReadFile,
		Build:      build,
		Probe: func(ctx context.Context, path string) (string, error) {
			return selfupdate.ProbeVersion(ctx, path, 10*time.Second)
		},
		LockTimeout: 2 * time.Second,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
	}
}

// Copy copies the file at src to dst with mode, creating dst's directory.
func Copy(tb testing.TB, src, dst string, mode os.FileMode) {
	tb.Helper()
	data, err := os.ReadFile(src) //nolint:gosec // test-controlled path
	if err != nil {
		tb.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		tb.Fatal(err)
	}
	if err := os.Chmod(dst, mode); err != nil {
		tb.Fatal(err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ErrFake is a ready error for fake verifiers and sources.
var ErrFake = errors.New("fake failure")

// Release returns a release spec for version whose archive holds a fake gskill
// for this platform reporting that version, with a matching checksums.txt and
// a placeholder signature bundle for a fake verifier.
func Release(tb testing.TB, version string) ReleaseSpec {
	tb.Helper()
	bin, err := os.ReadFile(BuildFake(tb, version, "ok"))
	if err != nil {
		tb.Fatal(err)
	}
	name := selfupdate.ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	archive := Archive(tb, Entry{Name: "gskill", Body: bin})
	return ReleaseSpec{Tag: "v" + version, Assets: map[string][]byte{
		name:                          archive,
		"checksums.txt":               Checksums(map[string][]byte{name: archive}),
		"checksums.txt.sigstore.json": []byte("{}"),
	}}
}
