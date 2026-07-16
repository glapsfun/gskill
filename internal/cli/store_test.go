package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/home"
	"github.com/glapsfun/gskill/internal/integrity"
)

// storeFixture builds a private home holding one healthy and one tampered
// object, returning the home path and both keys.
func storeFixture(t *testing.T) (homeDir, healthy, corrupted string) {
	t.Helper()
	homeDir = filepath.Join(t.TempDir(), "gskill-home")
	h := home.New(homeDir)
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	s := globalstore.New(h)

	admit := func(body string) string {
		dir := t.TempDir()
		md := "---\nname: fixture\ndescription: store fixture\n---\n" + body
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
			t.Fatal(err)
		}
		hashes, err := integrity.HashDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Admit(context.Background(), hashes.ContentHash, dir, globalstore.Origin{Commit: "c"}); err != nil {
			t.Fatal(err)
		}
		return hashes.ContentHash
	}
	healthy = admit("# healthy " + t.Name() + "\n")
	corrupted = admit("# corrupt " + t.Name() + "\n")
	if err := os.WriteFile(filepath.Join(s.ContentPath(corrupted), "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return homeDir, healthy, corrupted
}

// runStore runs the CLI with an App bound to homeDir.
func runStore(t *testing.T, homeDir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	a := app.New(app.Options{
		Agents:     agent.NewDefaultRegistry(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		GskillHome: homeDir,
	})
	var out, errb bytes.Buffer
	code = cli.Run(context.Background(), args, &out, &errb, a)
	return out.String(), errb.String(), code
}

// TestStoreVerify_ReportsCorruptionAndExitsNonZero (FR-022, contracts §2).
func TestStoreVerify_ReportsCorruptionAndExitsNonZero(t *testing.T) {
	t.Parallel()

	homeDir, _, corrupted := storeFixture(t)
	stdout, _, code := runStore(t, homeDir, "store", "verify")
	if code == 0 {
		t.Fatal("store verify exited 0 with a corrupted object present")
	}
	for _, want := range []string{"Checked: 2", "Healthy: 1", corrupted, "store repair"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("verify output missing %q:\n%s", want, stdout)
		}
	}
}

// TestStoreVerify_JSONShape (contracts §2): machine-readable findings.
func TestStoreVerify_JSONShape(t *testing.T) {
	t.Parallel()

	homeDir, _, corrupted := storeFixture(t)
	stdout, _, _ := runStore(t, homeDir, "store", "verify", "--json")
	var doc struct {
		Checked  int `json:"checked"`
		Healthy  int `json:"healthy"`
		Findings []struct {
			Kind     string `json:"kind"`
			Object   string `json:"object"`
			Expected string `json:"expected"`
			Actual   string `json:"actual"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("verify --json is not a JSON object: %v\n%s", err, stdout)
	}
	if doc.Checked != 2 || doc.Healthy != 1 || len(doc.Findings) != 1 {
		t.Fatalf("doc = %+v, want 2 checked / 1 healthy / 1 finding", doc)
	}
	f := doc.Findings[0]
	if f.Kind != "corrupted" || f.Object != corrupted || f.Expected != corrupted || f.Actual == "" {
		t.Errorf("finding = %+v", f)
	}
}

// TestStoreVerify_HealthyStoreExitsZero.
func TestStoreVerify_HealthyStoreExitsZero(t *testing.T) {
	t.Parallel()

	homeDir := filepath.Join(t.TempDir(), "gskill-home")
	if err := home.New(homeDir).Ensure(); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runStore(t, homeDir, "store", "verify")
	if code != 0 {
		t.Fatalf("empty store verify exit = %d (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "Checked: 0") {
		t.Errorf("output:\n%s", stdout)
	}
}

// TestStoreRepair_UnknownObjectFails (contracts §2).
func TestStoreRepair_UnknownObjectFails(t *testing.T) {
	t.Parallel()

	homeDir, _, _ := storeFixture(t)
	_, stderr, code := runStore(t, homeDir, "store", "repair", "sha256:0000000000000000")
	if code == 0 {
		t.Fatal("repairing an unknown object exited 0")
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr = %q, want not-found diagnostic", stderr)
	}
}

// TestStoreVerify_SanitizesUntrustedStrings (FR-034): origin-derived text
// cannot smuggle terminal escapes through the renderer.
func TestStoreVerify_SanitizesUntrustedStrings(t *testing.T) {
	t.Parallel()

	homeDir, _, corrupted := storeFixture(t)
	// Inject a hostile origin source into the corrupted object's metadata.
	h := home.New(homeDir)
	s := globalstore.New(h)
	meta, err := globalstore.ReadMetadata(s.MetadataPath(corrupted))
	if err != nil {
		t.Fatal(err)
	}
	meta.Origins = globalstore.MergeOrigins(meta.Origins, globalstore.Origin{
		Source: "evil\x1b[2Jsource", Commit: "x",
	})
	if err := globalstore.WriteMetadata(s.MetadataPath(corrupted), meta); err != nil {
		t.Fatal(err)
	}

	stdout, _, _ := runStore(t, homeDir, "store", "verify")
	if strings.Contains(stdout, "\x1b[2J") {
		t.Error("raw escape sequence leaked into store verify output")
	}
}
