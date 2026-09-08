package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

// homedApp builds a test App whose gskill home is an isolated temp directory,
// so the cache-path assertions never touch the developer's real ~/.gskill.
func homedApp(t *testing.T) (*app.App, string) {
	t.Helper()

	home := t.TempDir()
	return app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		GskillHome: home,
	}), home
}

// TestCacheStats_CarriesThePath is spec 025 FR-007: `cache path` was folded
// into `cache stats`, so the cache directory must remain reachable — as a
// named JSON field for scripts, and on its own line for humans.
func TestCacheStats_CarriesThePath(t *testing.T) {
	t.Parallel()

	a, home := homedApp(t)
	want := filepath.Join(home, "cache")

	stdout, stderr, code := runCLI(t, a, "--json", "cache", "stats")
	if code != 0 {
		t.Fatalf("cache stats --json: exit %d (stderr: %q)", code, stderr)
	}
	var got struct {
		Path  string `json:"path"`
		Files int    `json:"files"`
		Bytes int64  `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("cache stats --json is not valid JSON: %v\n%s", err, stdout)
	}
	if got.Path != want {
		t.Errorf("cache stats --json .path = %q, want %q", got.Path, want)
	}
	if !strings.Contains(stdout, `"files"`) || !strings.Contains(stdout, `"bytes"`) {
		t.Errorf("cache stats --json dropped an existing field:\n%s", stdout)
	}

	human, stderr, code := runCLI(t, a, "--no-interactive", "cache", "stats")
	if code != 0 {
		t.Fatalf("cache stats: exit %d (stderr: %q)", code, stderr)
	}
	if !strings.Contains(human, want) {
		t.Errorf("cache stats human output does not name the directory %q:\n%s", want, human)
	}
	if !strings.Contains(human, "file(s)") {
		t.Errorf("cache stats human output lost its counts:\n%s", human)
	}
}

// TestConfigList_CarriesThePath is the other half of FR-007: `config path`
// was folded into `config list`. The payload is nested under "values" rather
// than flattened, so the config-key namespace stays clean and `config get`
// keeps rejecting "path" as an unknown key (research R6).
func TestConfigList_CarriesThePath(t *testing.T) {
	// No t.Parallel: t.Setenv pins the config dir for this test.
	dir := t.TempDir()
	t.Setenv("GSKILL_CONFIG_DIR", dir)
	want := filepath.Join(dir, "config.toml")

	stdout, stderr, code := runCLI(t, newTestApp(), "--json", "config", "list")
	if code != 0 {
		t.Fatalf("config list --json: exit %d (stderr: %q)", code, stderr)
	}
	var got struct {
		Path   string            `json:"path"`
		Values map[string]string `json:"values"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("config list --json is not valid JSON: %v\n%s", err, stdout)
	}
	if got.Path != want {
		t.Errorf("config list --json .path = %q, want %q", got.Path, want)
	}
	for _, key := range []string{"log_level", "log_format", "offline", "no_cache", "jobs"} {
		if _, ok := got.Values[key]; !ok {
			t.Errorf("config list --json .values missing %q:\n%s", key, stdout)
		}
	}

	human, stderr, code := runCLI(t, newTestApp(), "--no-interactive", "config", "list")
	if code != 0 {
		t.Fatalf("config list: exit %d (stderr: %q)", code, stderr)
	}
	if !strings.HasPrefix(human, "# user config: "+want) {
		t.Errorf("config list human output does not lead with the config path %q:\n%s", want, human)
	}
	if !strings.Contains(human, "log_level = ") {
		t.Errorf("config list human output lost its key/value lines:\n%s", human)
	}

	// The config-key namespace must stay free of the folded path.
	if _, _, code := runCLI(t, newTestApp(), "config", "get", "path"); code == 0 {
		t.Error("config get path succeeded; the folded path must not become a config key")
	}
	if _, stderr, code := runCLI(t, newTestApp(), "config", "get", "log_level"); code != 0 {
		t.Errorf("config get log_level: exit %d, want 0 (stderr: %q)", code, stderr)
	}
}
