package app

import (
	"reflect"
	"testing"

	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
)

// TestBackfillPins covers the manifest auto-recording rules (FR-001..FR-006):
// the resolved revision maps to a manifest field by ref-kind, explicit values
// are preserved, and the agent set is recorded per-skill unless inherited from a
// [defaults] agents block.
func TestBackfillPins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		in                manifest.Skill
		rev               resolver.Revision
		resolvedAgentIDs  []string
		hasDefaultsAgents bool
		want              manifest.Skill
	}{
		{
			name:             "semver fills caret version range",
			in:               manifest.Skill{Source: "s"},
			rev:              resolver.Revision{RefKind: resolver.RefKindSemver, Version: "0.1.0"},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Version: "^0.1.0", Agents: []string{"claude"}},
		},
		{
			name:             "non-semver tag fills ref",
			in:               manifest.Skill{Source: "s"},
			rev:              resolver.Revision{RefKind: resolver.RefKindTag, Tag: "v1.0.0-beta"},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Ref: "v1.0.0-beta", Agents: []string{"claude"}},
		},
		{
			name:             "branch fills ref",
			in:               manifest.Skill{Source: "s"},
			rev:              resolver.Revision{RefKind: resolver.RefKindBranch, Branch: "main"},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Ref: "main", Agents: []string{"claude"}},
		},
		{
			name:             "commit fills commit",
			in:               manifest.Skill{Source: "s"},
			rev:              resolver.Revision{RefKind: resolver.RefKindCommit, Commit: "abc123"},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Commit: "abc123", Agents: []string{"claude"}},
		},
		{
			name:             "local fills no pin but keeps agents",
			in:               manifest.Skill{Source: "s"},
			rev:              resolver.Revision{RefKind: resolver.RefKindLocal},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Agents: []string{"claude"}},
		},
		{
			name:             "explicit version preserved (no overwrite)",
			in:               manifest.Skill{Source: "s", Version: "^1.0.0"},
			rev:              resolver.Revision{RefKind: resolver.RefKindSemver, Version: "1.2.3"},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Version: "^1.0.0", Agents: []string{"claude"}},
		},
		{
			name:             "explicit ref preserved (no pin overwrite)",
			in:               manifest.Skill{Source: "s", Ref: "develop"},
			rev:              resolver.Revision{RefKind: resolver.RefKindBranch, Branch: "main"},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Ref: "develop", Agents: []string{"claude"}},
		},
		{
			name:             "explicit agents preserved",
			in:               manifest.Skill{Source: "s", Agents: []string{"codex"}},
			rev:              resolver.Revision{RefKind: resolver.RefKindSemver, Version: "0.1.0"},
			resolvedAgentIDs: []string{"claude"},
			want:             manifest.Skill{Source: "s", Version: "^0.1.0", Agents: []string{"codex"}},
		},
		{
			name:              "defaults agents block suppresses per-skill agents",
			in:                manifest.Skill{Source: "s"},
			rev:               resolver.Revision{RefKind: resolver.RefKindSemver, Version: "0.1.0"},
			resolvedAgentIDs:  []string{"claude", "codex"},
			hasDefaultsAgents: true,
			want:              manifest.Skill{Source: "s", Version: "^0.1.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := backfillPins(tt.in, tt.rev, tt.resolvedAgentIDs, tt.hasDefaultsAgents)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("backfillPins() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
