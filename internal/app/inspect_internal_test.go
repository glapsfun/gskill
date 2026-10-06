package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/active"
)

// TestVerifySkill_SharedTargets covers spec 027: several shared-location
// targets name the same active entry, which verify reports on exactly as it
// would for one target.
func TestVerifySkill_SharedTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := openProject(root)
	hash, storePath := seedStore(t, p)
	arrangeActiveOK(t, root, storePath, hash)
	rec := lockWithShared("demo", hash, "sharedfake", "sharedfake2").Skills["demo"]

	if got := verifySkill(root, "demo", rec); !got.OK || got.Issue != "ok" {
		t.Fatalf("verify on intact content = %+v, want ok", got)
	}

	if err := os.WriteFile(filepath.Join(active.Path(root, "demo"), "SKILL.md"), []byte("# tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := verifySkill(root, "demo", rec)
	if got.OK || got.Issue != "mismatch" || got.Actual == "" {
		t.Errorf("verify on tampered content = %+v, want a mismatch with the actual hash", got)
	}
}
