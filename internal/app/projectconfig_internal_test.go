package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreLockTimeout_KeepsUserFileLayerUnderProjectConfig guards the one
// place that still resolves configuration a second time.
//
// storeLockTimeout re-resolves through projectConfig so a project can declare
// its own contention window. That second resolution must carry the same user
// config file the run resolved with — otherwise any project whose skills.toml
// declares a [config] table at all, even one silent about locking, would
// replace the fully-resolved configuration with one missing the user layer
// and quietly revert a configured store.lock_timeout to the 60s default
// (spec 026 FR-008).
func TestStoreLockTimeout_KeepsUserFileLayerUnderProjectConfig(t *testing.T) {
	t.Parallel()

	userFile := filepath.Join(t.TempDir(), "user.toml")
	if err := os.WriteFile(userFile, []byte("[store]\nlock_timeout = \"90s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	// A project [config] table that says nothing about locking is the case
	// that matters: it is enough to trigger the re-resolution.
	if err := os.WriteFile(filepath.Join(root, "skills.toml"), []byte("[config]\nlog_level = \"warn\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := New(Options{})
	if err := a.ApplyRuntimeConfig(root, userFile); err != nil {
		t.Fatalf("ApplyRuntimeConfig: %v", err)
	}
	if got := a.Config().StoreLockTimeout; got != 90*time.Second {
		t.Fatalf("precondition: Config().StoreLockTimeout = %v, want 90s", got)
	}
	if got := a.storeLockTimeout(root); got != 90*time.Second {
		t.Errorf("storeLockTimeout = %v, want 90s — the user-file layer was dropped by the second resolution", got)
	}
}
