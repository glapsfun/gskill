package skillslock

import (
	"reflect"
	"testing"
)

func fullRecord() Record {
	return Record{
		Source: Source{
			Type: "github", Original: "owner/repo", URL: "https://github.com/owner/repo.git",
			Owner: "owner", Repo: "repo", Path: "skills/x",
		},
		Requested: Requested{Version: "^1.0.0", Ref: "main", Commit: ""},
		Resolved: Resolved{
			Version: "1.2.0", RefKind: "branch", Branch: "main", Commit: "abc123",
			TreeHash: "t1", ContentHash: "sha256:store", SkillFileHash: "sha256:skill",
			MutableRef: true, CompatHash: "03e0eaaa",
		},
		Metadata: Metadata{Name: "x", Description: "d", Version: "1.2.0", License: "MIT"},
		Requires: Requires{Skills: []string{"y"}, Commands: []string{"git"}},
		Installation: Installation{
			Scope: "project", Mode: "symlink", Agents: []string{"claude", "codex"},
			ActivePath: ".agents/skills/x",
			Targets:    map[string]string{"claude": ".claude/skills/x"},
			Modes:      map[string]string{"claude": "symlink"},
		},
		Provenance: Provenance{FetchedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:00Z", Trust: "tofu"},
	}
}

// A Record survives the trip through the shared-format Entry unchanged.
func TestRecordEntryRoundTrip(t *testing.T) {
	t.Parallel()
	in := fullRecord()
	e := FromRecord(in)
	out := ToRecord("x", e)
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip changed record:\n in: %+v\nout: %+v", in, out)
	}
}

// An external-only entry (no gskill block) still produces a usable Record.
func TestToRecordExternalOnly(t *testing.T) {
	t.Parallel()
	e := Entry{
		Source: "owner/repo", SourceType: "github",
		SkillPath: "skills/x/SKILL.md", ComputedHash: "03e0eaaa",
	}
	r := ToRecord("x", e)
	if r.Source.Owner != "owner" || r.Source.Repo != "repo" {
		t.Fatalf("owner/repo not derived: %+v", r.Source)
	}
	if r.Metadata.Name != "x" {
		t.Fatalf("name fallback missing: %q", r.Metadata.Name)
	}
	if r.Resolved.CompatHash != "03e0eaaa" {
		t.Fatalf("computedHash not carried: %q", r.Resolved.CompatHash)
	}
}
