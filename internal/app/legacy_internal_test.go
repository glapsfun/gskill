package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// A gskill.lock on disk is inert foreign data: never read, never deleted.
func TestLegacyGskillLockIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	legacy := filepath.Join(root, "gskill.lock")
	legacyBody := []byte(`{"lockfile_version":1,"skills":{"ghost":{}}}`)
	if err := os.WriteFile(legacy, legacyBody, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loadOrNewLock(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatalf("loadOrNewLock: %v", err)
	}
	if len(st.Skills) != 0 {
		t.Fatalf("legacy gskill.lock was read: %v", st.Skills)
	}
	got, err := os.ReadFile(legacy) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("legacy file must be preserved untouched: %v", err)
	}
	if string(got) != string(legacyBody) {
		t.Fatal("legacy file content changed")
	}
}
