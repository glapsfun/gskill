package skillslock_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// fullLegacy is a fully populated legacy record exercising every mapped field
// (data-model migration table).
func fullLegacy() skillslock.Record {
	return skillslock.Record{
		Source: skillslock.Source{
			Type:     "github",
			Original: "vercel-labs/agent-skills",
			URL:      "https://github.com/vercel-labs/agent-skills.git",
			Owner:    "vercel-labs",
			Repo:     "agent-skills",
			Path:     "skills/deploy-to-vercel",
		},
		Requested: skillslock.Requested{Version: "^1.0.0", Ref: "main", Commit: "req-commit"},
		Resolved: skillslock.Resolved{
			Version:       "1.2.0",
			RefKind:       "tag",
			Tag:           "v1.2.0",
			Branch:        "main",
			Commit:        "abc123def456",
			TreeHash:      "tree789",
			ContentHash:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			SkillFileHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			MutableRef:    true,
			LocalPathHash: "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		},
		Metadata: skillslock.Metadata{
			Name: "deploy-to-vercel", Description: "Deploys", Version: "1.2.0", License: "MIT",
		},
		Requires: skillslock.Requires{
			Skills:      []string{"other-skill"},
			Commands:    []string{"vercel"},
			Environment: []string{"VERCEL_TOKEN"},
			MCP:         []string{"vercel-mcp"},
		},
		Installation: skillslock.Installation{
			Scope:      "project",
			Mode:       "symlink",
			Agents:     []string{"claude", "codex"},
			ActivePath: ".agents/skills/deploy-to-vercel",
			Targets: map[string]string{
				"claude": ".claude/skills/deploy-to-vercel",
				"codex":  ".codex/skills/deploy-to-vercel",
			},
			Modes: map[string]string{"claude": "symlink", "codex": "copy"},
		},
		Provenance: skillslock.Provenance{
			FetchedAt: "2026-07-10T12:00:00Z", UpdatedAt: "2026-07-10T12:30:00Z", Trust: "checksum-ok",
		},
	}
}

func TestFromLegacyCoreFields(t *testing.T) {
	t.Parallel()
	e := skillslock.FromRecord(fullLegacy())
	assertCore(t, e)
	assertExt(t, e)
}

func assertCore(t *testing.T, e skillslock.Entry) {
	t.Helper()
	if e.Source != "vercel-labs/agent-skills" {
		t.Errorf("Source = %q, want owner/repo shorthand", e.Source)
	}
	if e.SourceType != srcTypeGitHub {
		t.Errorf("SourceType = %q", e.SourceType)
	}
	if e.SkillPath != "skills/deploy-to-vercel/SKILL.md" {
		t.Errorf("SkillPath = %q", e.SkillPath)
	}
	if e.ComputedHash != "" {
		t.Errorf("ComputedHash = %q, want empty (not derivable from legacy record)", e.ComputedHash)
	}
}

func assertExt(t *testing.T, e skillslock.Entry) {
	t.Helper()
	if e.Ext == nil {
		t.Fatal("Ext missing")
	}
	fields := []struct{ name, got, want string }{
		{"SourceURL", e.Ext.SourceURL, "https://github.com/vercel-labs/agent-skills.git"},
		{"Ref (resolved tag)", e.Ext.Ref, "v1.2.0"},
		{"Commit", e.Ext.Commit, "abc123def456"},
		{"Version", e.Ext.Version, "1.2.0"},
		{"Agents", strings.Join(e.Ext.Agents, ","), "claude,codex"},
		{"InstallMode", e.Ext.InstallMode, "symlink"},
		{"Scope", e.Ext.Scope, "project"},
		{"StoreHash", e.Ext.StoreHash, "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		{"SkillFileHash", e.Ext.SkillFileHash, "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
		{"InstalledAt", e.Ext.InstalledAt, "2026-07-10T12:00:00Z"},
		{"UpdatedAt", e.Ext.UpdatedAt, "2026-07-10T12:30:00Z"},
	}
	for _, f := range fields {
		if f.got != f.want {
			t.Errorf("Ext.%s = %q, want %q", f.name, f.got, f.want)
		}
	}
}

func TestBridgeRoundTripInMemory(t *testing.T) {
	t.Parallel()
	want := fullLegacy()
	got := skillslock.ToRecord("deploy-to-vercel", skillslock.FromRecord(want))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// TestBridgeRoundTripThroughJSON proves the mapping survives serialization:
// legacy -> entry -> skills-lock.json bytes -> entry -> legacy.
func TestBridgeRoundTripThroughJSON(t *testing.T) {
	t.Parallel()
	want := fullLegacy()
	l := skillslock.New()
	l.SetEntry("deploy-to-vercel", skillslock.FromRecord(want))

	data, err := skillslock.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	l2, err := skillslock.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	e, ok := l2.Entry("deploy-to-vercel")
	if !ok {
		t.Fatal("entry lost")
	}
	got := skillslock.ToRecord("deploy-to-vercel", e)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// TestBridgeMinimalEntry: an external-only entry (no gskill block) still maps
// to a usable legacy record for source resolution.
func TestBridgeMinimalEntry(t *testing.T) {
	t.Parallel()
	e := skillslock.Entry{
		Source:       "vercel-labs/agent-skills",
		SourceType:   "github",
		SkillPath:    "skills/deploy-to-vercel/SKILL.md",
		ComputedHash: "03e0eaaa9bf13ba1e7ffa387f5893de6f324c0868c627001f179395a8feaa7c9",
	}
	ls := skillslock.ToRecord("deploy-to-vercel", e)
	if ls.Source.Type != srcTypeGitHub {
		t.Errorf("Source.Type = %q", ls.Source.Type)
	}
	if ls.Source.Owner != "vercel-labs" || ls.Source.Repo != "agent-skills" {
		t.Errorf("owner/repo = %q/%q", ls.Source.Owner, ls.Source.Repo)
	}
	if ls.Source.Path != "skills/deploy-to-vercel" {
		t.Errorf("Source.Path = %q, want dir of skillPath", ls.Source.Path)
	}
}

// TestBridgeLocalSource: non-github types survive via state, not owner/repo.
func TestBridgeLocalSource(t *testing.T) {
	t.Parallel()
	ls := skillslock.Record{
		Source: skillslock.Source{Type: "local", Original: "../skills-repo", Path: "my-skill"},
		Resolved: skillslock.Resolved{
			RefKind:     "local",
			ContentHash: "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		},
		Metadata: skillslock.Metadata{Name: "my-skill", Description: "Local"},
	}
	e := skillslock.FromRecord(ls)
	if e.Source != "../skills-repo" {
		t.Errorf("Source = %q, want original for non-github", e.Source)
	}
	if e.SourceType != "local" {
		t.Errorf("SourceType = %q", e.SourceType)
	}
	got := skillslock.ToRecord("my-skill", e)
	if !reflect.DeepEqual(got, ls) {
		t.Errorf("local round trip mismatch:\n got %+v\nwant %+v", got, ls)
	}
}
