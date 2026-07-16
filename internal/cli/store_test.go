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

// TestStoreList_JSONShapeAndSanitize (contracts §2, FR-034).
func TestStoreList_JSONShapeAndSanitize(t *testing.T) {
	t.Parallel()

	homeDir, healthy, _ := storeFixture(t)
	// Hostile origin skill path on the healthy object.
	h := home.New(homeDir)
	s := globalstore.New(h)
	meta, err := globalstore.ReadMetadata(s.MetadataPath(healthy))
	if err != nil {
		t.Fatal(err)
	}
	meta.Origins = globalstore.MergeOrigins(meta.Origins, globalstore.Origin{
		SkillPath: "skills/evil\x1b[2Jname", Version: "1.0.0", Commit: "x",
	})
	if err := globalstore.WriteMetadata(s.MetadataPath(healthy), meta); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runStore(t, homeDir, "store", "list")
	if code != 0 {
		t.Fatalf("store list exit = %d", code)
	}
	if strings.Contains(stdout, "\x1b[2J") {
		t.Error("raw escape sequence leaked into store list output")
	}

	jsonOut, _, _ := runStore(t, homeDir, "store", "list", "--json")
	var doc struct {
		Objects []struct {
			Hash      string `json:"hash"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("list --json: %v\n%s", err, jsonOut)
	}
	if len(doc.Objects) != 2 {
		t.Errorf("objects = %+v, want 2", doc.Objects)
	}
}

// TestStoreGC_DryRunByDefault (FR-025): without --apply nothing is deleted
// and the hint points at --apply.
func TestStoreGC_DryRunByDefault(t *testing.T) {
	t.Parallel()

	homeDir, healthy, corrupted := storeFixture(t)
	stdout, _, code := runStore(t, homeDir, "store", "gc", "--older-than", "0d")
	if code != 0 {
		t.Fatalf("gc dry-run exit = %d\n%s", code, stdout)
	}
	// Nothing deleted: both objects still present (the corrupted one has
	// invalid content but valid metadata, so it may be a candidate — either
	// way, a dry run removes nothing).
	h := home.New(homeDir)
	s := globalstore.New(h)
	for _, key := range []string{healthy, corrupted} {
		if !s.Has(key) {
			t.Errorf("dry-run gc removed %s", key)
		}
	}
	if !strings.Contains(stdout, "--apply") {
		t.Errorf("dry-run output has no --apply hint:\n%s", stdout)
	}
}

// TestStorePins_RoundTripCLI (FR-026).
func TestStorePins_RoundTripCLI(t *testing.T) {
	t.Parallel()

	homeDir, healthy, _ := storeFixture(t)
	if _, stderr, code := runStore(t, homeDir, "store", "pin", healthy); code != 0 {
		t.Fatalf("pin: %s", stderr)
	}
	stdout, _, _ := runStore(t, homeDir, "store", "pins")
	if !strings.Contains(stdout, healthy) {
		t.Errorf("pins output missing %s:\n%s", healthy, stdout)
	}
	if _, stderr, code := runStore(t, homeDir, "store", "unpin", healthy); code != 0 {
		t.Fatalf("unpin: %s", stderr)
	}
	stdout, _, _ = runStore(t, homeDir, "store", "pins")
	if strings.Contains(stdout, healthy) {
		t.Errorf("pins output still lists %s after unpin", healthy)
	}
}
