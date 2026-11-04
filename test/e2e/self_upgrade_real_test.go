//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireSelfUpgradeE2E skips unless the opt-in flag is set. Unlike requireE2E
// it needs no git: the test only talks to GitHub Releases and Sigstore.
func requireSelfUpgradeE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("GSKILL_E2E") != "1" {
		t.Skip("set GSKILL_E2E=1 to run the real self-upgrade test")
	}
}

// latestStable reads the newest stable gskill release tag from GitHub.
func latestStable(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/glapsfun/gskill/releases/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil || rel.TagName == "" {
		t.Fatalf("read latest release: status %d, err %v", resp.StatusCode, err)
	}
	return rel.TagName
}

// TestE2E_SelfUpgradeToLatestStable builds this checkout as release 0.8.0 and
// upgrades it to the live latest release: GitHub metadata, the real archive,
// the real Sigstore signature, and the real TUF root.
func TestE2E_SelfUpgradeToLatestStable(t *testing.T) {
	requireSelfUpgradeE2E(t)

	latest := strings.TrimPrefix(latestStable(t), "v")
	bin := filepath.Join(t.TempDir(), "bin")
	exe := filepath.Join(bin, "gskill")
	ldflags := "-s -w -X github.com/glapsfun/gskill/internal/version.version=0.8.0 -X github.com/glapsfun/gskill/internal/version.commit=e2e"
	build := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", exe, "../../cmd/gskill")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gskill 0.8.0: %v\n%s", err, out)
	}

	cmd := exec.Command(exe, "--yes", "--no-interactive", "--json", "self", "upgrade")
	cmd.Env = append(os.Environ(), "GSKILL_CACHE_DIR="+t.TempDir())
	cmd.Stdin = nil
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("self upgrade: %v\nstdout %s\nstderr %s", err, stdout, stderr.String())
	}
	var outcome struct {
		Outcome          string `json:"outcome"`
		InstalledVersion string `json:"installed_version"`
	}
	if err := json.Unmarshal(stdout, &outcome); err != nil {
		t.Fatalf("decode %s: %v", stdout, err)
	}
	if outcome.Outcome != "upgraded" || outcome.InstalledVersion != latest {
		t.Errorf("outcome = %+v, want upgraded to %s", outcome, latest)
	}

	out, err := exec.Command(exe, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "gskill "+latest {
		t.Errorf("gskill version = %q, want gskill %s", got, latest)
	}
	entries, err := os.ReadDir(bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%s holds %d entries, want only gskill", bin, len(entries))
	}
}
