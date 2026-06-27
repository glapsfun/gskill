package lockfile_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
)

func consistentPair() (*manifest.Manifest, *lockfile.Lockfile) {
	m := manifest.New()
	m.Skills["demo"] = manifest.Skill{Source: "github.com/acme/demo", Version: "^1.0.0"}

	lf := lockfile.New()
	lf.Skills["demo"] = lockfile.LockedSkill{
		Source:    lockfile.Source{Type: "git", Original: "github.com/acme/demo"},
		Requested: lockfile.Requested{Version: "^1.0.0"},
		Resolved:  lockfile.Resolved{RefKind: "semver", Commit: "abc", ContentHash: "sha256:x"},
	}
	return m, lf
}

func TestCheckConsistency_Agrees(t *testing.T) {
	t.Parallel()

	m, lf := consistentPair()
	if err := lockfile.CheckConsistency(m, lf); err != nil {
		t.Errorf("CheckConsistency = %v, want nil", err)
	}
}

func TestCheckConsistency_Disagreements(t *testing.T) {
	t.Parallel()

	t.Run("declared but not locked", func(t *testing.T) {
		t.Parallel()
		m, lf := consistentPair()
		m.Skills["extra"] = manifest.Skill{Source: "github.com/acme/extra"}
		assertMismatch(t, m, lf)
	})

	t.Run("locked but not declared", func(t *testing.T) {
		t.Parallel()
		m, lf := consistentPair()
		delete(m.Skills, "demo")
		assertMismatch(t, m, lf)
	})

	t.Run("source changed", func(t *testing.T) {
		t.Parallel()
		m, lf := consistentPair()
		m.Skills["demo"] = manifest.Skill{Source: "github.com/acme/other", Version: "^1.0.0"}
		assertMismatch(t, m, lf)
	})

	t.Run("requested version changed", func(t *testing.T) {
		t.Parallel()
		m, lf := consistentPair()
		m.Skills["demo"] = manifest.Skill{Source: "github.com/acme/demo", Version: "^2.0.0"}
		assertMismatch(t, m, lf)
	})
}

func assertMismatch(t *testing.T, m *manifest.Manifest, lf *lockfile.Lockfile) {
	t.Helper()

	err := lockfile.CheckConsistency(m, lf)
	if err == nil {
		t.Fatal("expected a mismatch error")
	}
	if got := errs.ExitCode(err); got != 4 {
		t.Errorf("exit code = %d, want 4", got)
	}
}
