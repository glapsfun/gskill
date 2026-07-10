package skillslock_test

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// TestMigrateFromLegacyConvertsEveryEntry (T032/FR-008): a populated legacy
// lockfile converts per the data-model mapping: compatible core fields plus a
// namespaced gskill block, computedHash omitted until content is hashed.
func TestMigrateFromLegacyConvertsEveryEntry(t *testing.T) {
	t.Parallel()
	legacy := lockfile.New()
	legacy.Skills["deploy-to-vercel"] = fullLegacy()
	second := fullLegacy()
	second.Source.Path = "skills/web-design"
	legacy.Skills["web-design"] = second

	l := skillslock.MigrateFromLegacy(legacy)

	names := l.Names()
	if len(names) != 2 || names[0] != "deploy-to-vercel" || names[1] != "web-design" {
		t.Fatalf("Names() = %v, want both entries sorted", names)
	}
	e, ok := l.Entry("deploy-to-vercel")
	if !ok {
		t.Fatal("entry missing")
	}
	if e.Source != "vercel-labs/agent-skills" || e.SourceType != srcTypeGitHub {
		t.Errorf("core = %q/%q", e.Source, e.SourceType)
	}
	if e.SkillPath != "skills/deploy-to-vercel/SKILL.md" {
		t.Errorf("SkillPath = %q", e.SkillPath)
	}
	if e.ComputedHash != "" {
		t.Errorf("ComputedHash = %q, want omitted (not derivable)", e.ComputedHash)
	}
	if e.Ext == nil || e.Ext.StoreHash == "" || len(e.Ext.Agents) != 2 {
		t.Errorf("gskill block incomplete: %+v", e.Ext)
	}

	// The migrated lock validates: entries carrying gskill metadata may omit
	// computedHash until the next install records it.
	if err := l.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for gskill-managed entries", err)
	}
}

// TestValidateRequiresHashForExternalOnly: entries without gskill metadata
// still require computedHash (it is their only integrity anchor).
func TestValidateRequiresHashForExternalOnly(t *testing.T) {
	t.Parallel()
	l := skillslock.New()
	l.SetEntry("bare", skillslock.Entry{
		Source: "o/r", SourceType: "github", SkillPath: "skills/bare/SKILL.md",
	})
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), "computedHash") {
		t.Fatalf("Validate() = %v, want missing computedHash", err)
	}
}

// TestMigrateRoundTripsThroughLegacyView: migration output feeds the same
// bridge the app uses, so migrated state keeps driving existing commands.
func TestMigrateRoundTripsThroughLegacyView(t *testing.T) {
	t.Parallel()
	legacy := lockfile.New()
	legacy.Skills["deploy-to-vercel"] = fullLegacy()

	l := skillslock.MigrateFromLegacy(legacy)
	e, _ := l.Entry("deploy-to-vercel")
	back := skillslock.ToLegacy("deploy-to-vercel", e)
	if back.Resolved.Commit != "abc123def456" || back.Installation.Targets["claude"] == "" {
		t.Errorf("legacy view degraded after migration: %+v", back)
	}
}
