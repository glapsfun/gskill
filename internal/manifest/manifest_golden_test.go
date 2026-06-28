package manifest_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/testutil"
)

func sampleManifest() *manifest.Manifest {
	m := manifest.New()
	m.Defaults = manifest.Defaults{
		Agents:      []string{"claude", "codex"},
		InstallMode: "symlink",
		Scope:       "project",
	}
	m.Skills["kubernetes-expert"] = manifest.Skill{
		Source:  "github.com/acme/widgets",
		Path:    "skills/kubernetes-expert",
		Version: "^2.0.0",
		Agents:  []string{"claude"},
	}
	return m
}

func TestMarshal_Golden(t *testing.T) {
	t.Parallel()

	got, err := manifest.Marshal(sampleManifest())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	testutil.Golden(t, "manifest.golden", got)
}

func TestRoundTrip_LoadSaveStable(t *testing.T) {
	t.Parallel()

	first, err := manifest.Marshal(sampleManifest())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	parsed, err := manifest.Unmarshal(first)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	second, err := manifest.Marshal(parsed)
	if err != nil {
		t.Fatalf("Marshal parsed: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("round trip not stable:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestUnmarshal_RefusesNewerSchema(t *testing.T) {
	t.Parallel()

	_, err := manifest.Unmarshal([]byte("schema_version = 2\n"))
	if err == nil {
		t.Fatal("expected refusal of newer schema_version")
	}
	if !errors.Is(err, manifest.ErrUnsupportedSchema) {
		t.Errorf("error = %v, want ErrUnsupportedSchema", err)
	}
}

func TestUnmarshal_RejectsUnknownTopLevel(t *testing.T) {
	t.Parallel()

	_, err := manifest.Unmarshal([]byte("schema_version = 1\n[unknown_section]\nx = 1\n"))
	if err == nil {
		t.Fatal("expected rejection of unknown top-level section")
	}
	if !errors.Is(err, manifest.ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}
}

func TestUnmarshal_RejectsBadSkillName(t *testing.T) {
	t.Parallel()

	_, err := manifest.Unmarshal([]byte("schema_version = 1\n[skills.Bad_Name]\nsource = \"x/y\"\n"))
	if err == nil {
		t.Fatal("expected rejection of non-kebab skill key")
	}
	if !errors.Is(err, manifest.ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}
}

func TestUnmarshal_RejectsMissingSource(t *testing.T) {
	t.Parallel()

	_, err := manifest.Unmarshal([]byte("schema_version = 1\n[skills.alpha]\nversion = \"1.0.0\"\n"))
	if err == nil {
		t.Fatal("expected rejection of skill without source")
	}
	if !errors.Is(err, manifest.ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}
}
