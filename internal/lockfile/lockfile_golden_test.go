package lockfile_test

import (
	"bytes"
	"testing"

	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/testutil"
)

// sampleLockfile is a fully populated lockfile used to pin deterministic
// serialization.
func sampleLockfile() *lockfile.Lockfile {
	lf := lockfile.New()
	lf.Skills["kubernetes-expert"] = lockfile.LockedSkill{
		Source: lockfile.Source{
			Type:     "git",
			Original: "github.com/acme/widgets/kubernetes-expert",
			URL:      "https://github.com/acme/widgets.git",
			Owner:    "acme",
			Repo:     "widgets",
			Path:     "kubernetes-expert",
		},
		Requested: lockfile.Requested{Version: "^2.0.0"},
		Resolved: lockfile.Resolved{
			Version:       "2.1.3",
			RefKind:       "semver",
			Tag:           "v2.1.3",
			Commit:        "6c58cfd49a71d86d7d225c61ea63d98c3df19bd1",
			ContentHash:   "sha256:aaaa",
			SkillFileHash: "sha256:bbbb",
			MutableRef:    false,
		},
		Metadata: lockfile.Metadata{
			Name:        "kubernetes-expert",
			Description: "Kubernetes operational guidance",
			License:     "MIT",
		},
		Requires: lockfile.Requires{
			Skills:      []string{"shell-scripting >=1.2.0"},
			Commands:    []string{"kubectl", "helm"},
			Environment: []string{"KUBECONFIG"},
			MCP:         []string{},
		},
		Installation: lockfile.Installation{
			Scope:      "project",
			Mode:       "symlink",
			Agents:     []string{"claude", "codex"},
			ActivePath: ".agents/skills/kubernetes-expert",
			Targets: map[string]string{
				"claude": ".claude/skills/kubernetes-expert",
				"codex":  ".codex/skills/kubernetes-expert",
			},
			Modes: map[string]string{
				"claude": "symlink",
				"codex":  "symlink",
			},
		},
		Provenance: lockfile.Provenance{Trust: "unverified"},
	}
	return lf
}

func TestMarshal_DeterministicGolden(t *testing.T) {
	t.Parallel()

	got, err := lockfile.Marshal(sampleLockfile())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	testutil.Golden(t, "lock.golden", got)

	if !bytes.HasSuffix(got, []byte("\n")) {
		t.Error("lockfile must end with a trailing newline")
	}
}

func TestMarshal_StableAcrossRuns(t *testing.T) {
	t.Parallel()

	a, err := lockfile.Marshal(sampleLockfile())
	if err != nil {
		t.Fatalf("Marshal a: %v", err)
	}
	b, err := lockfile.Marshal(sampleLockfile())
	if err != nil {
		t.Fatalf("Marshal b: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("Marshal output not stable across runs")
	}
}

func TestRoundTrip_LoadSave(t *testing.T) {
	t.Parallel()

	data, err := lockfile.Marshal(sampleLockfile())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	parsed, err := lockfile.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	again, err := lockfile.Marshal(parsed)
	if err != nil {
		t.Fatalf("Marshal parsed: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Errorf("round trip not stable:\n--- first ---\n%s\n--- second ---\n%s", data, again)
	}
}

func TestUnmarshal_RefusesNewerSchema(t *testing.T) {
	t.Parallel()

	if _, err := lockfile.Unmarshal([]byte(`{"lockfile_version": 2, "skills": {}}`)); err == nil {
		t.Error("expected refusal of newer lockfile_version")
	}
}
