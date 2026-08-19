package overrides_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/overrides"
)

// TestApply_PathConfinement is the security property from research R2. A patch
// is a committed file that arrives with every clone, so a diff naming a path
// outside its skill would write into a teammate's agent configuration on every
// machine. Every case must be refused before any write.
func TestApply_PathConfinement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec func(repo string) overrides.Spec
		repo map[string]string
	}{
		{
			name: "patch escapes via parent traversal",
			repo: map[string]string{
				"g/evil.diff": "--- a/../../.claude/settings.json\n+++ b/../../.claude/settings.json\n" +
					"@@ -1 +1 @@\n-{}\n+{\"pwned\":true}\n",
			},
			spec: func(string) overrides.Spec { return overrides.Spec{Patch: []string{"g/evil.diff"}} },
		},
		{
			name: "patch escapes via absolute path",
			repo: map[string]string{
				"g/abs.diff": "--- a//etc/passwd\n+++ b//etc/passwd\n@@ -1 +1 @@\n-x\n+y\n",
			},
			spec: func(string) overrides.Spec { return overrides.Spec{Patch: []string{"g/abs.diff"}} },
		},
		{
			name: "replace target escapes the skill",
			repo: map[string]string{"g/payload.md": "payload\n"},
			spec: func(string) overrides.Spec {
				return overrides.Spec{Replace: map[string]string{"../../escaped.md": "g/payload.md"}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			skill := skillDir(t, map[string]string{"SKILL.md": "safe\n"})
			repo := repoWith(t, tt.repo)
			outside := filepath.Join(filepath.Dir(skill), "escaped.md")

			err := overrides.Apply(skill, repo, tt.spec(repo))
			if err == nil {
				t.Fatal("escaping path must be refused")
			}
			if !errors.Is(err, errs.ErrIntegrity) {
				t.Errorf("want exit code 6 (ErrIntegrity), got %v", err)
			}
			if _, statErr := os.Stat(outside); statErr == nil {
				t.Error("refused override still wrote outside the skill directory")
			}
			if got := read(t, skill, "SKILL.md"); got != "safe\n" {
				t.Errorf("skill content modified despite refusal: %q", got)
			}
		})
	}
}

// TestApply_SymlinkEscapeRefused: confinement must resolve symlinks, not just
// inspect the path string.
func TestApply_SymlinkEscapeRefused(t *testing.T) {
	t.Parallel()

	skill := skillDir(t, map[string]string{"SKILL.md": "safe\n"})
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(skill, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	repo := repoWith(t, map[string]string{"g/p.md": "payload\n"})

	err := overrides.Apply(skill, repo, overrides.Spec{
		Replace: map[string]string{"out/escaped.md": "g/p.md"},
	})
	if err == nil {
		t.Fatal("symlinked escape must be refused")
	}
	if _, statErr := os.Stat(filepath.Join(outsideDir, "escaped.md")); statErr == nil {
		t.Error("wrote through a symlink out of the skill directory")
	}
}
