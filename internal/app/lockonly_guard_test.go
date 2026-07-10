package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// The full command lifecycle never creates or reads gskill.toml/gskill.lock.
func TestNoLegacyFilesEverCreated(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	ctx := context.Background()
	mustNoLegacy := func(step string) {
		t.Helper()
		for _, f := range []string{"gskill.toml", "gskill.lock"} {
			if _, err := os.Stat(filepath.Join(root, f)); !os.IsNotExist(err) {
				t.Fatalf("%s created %s", step, f)
			}
		}
	}
	if _, err := a.Init(ctx, root, true); err != nil {
		t.Fatal(err)
	}
	mustNoLegacy("init")
	if _, err := a.Add(ctx, app.AddRequest{
		Root: root, Source: src,
		Selectors: []string{"demo-skill"}, Agents: []string{"claude"},
	}); err != nil {
		t.Fatal(err)
	}
	mustNoLegacy("add")
	if _, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root}); err != nil {
		t.Fatal(err)
	}
	mustNoLegacy("install")
	if _, err := a.Update(ctx, root, nil); err != nil {
		t.Fatal(err)
	}
	mustNoLegacy("update")
	if _, err := a.Sync(ctx, app.SyncRequest{Root: root}); err != nil {
		t.Fatal(err)
	}
	mustNoLegacy("sync")
	if _, err := a.Remove(ctx, root, []string{"demo-skill"}); err != nil {
		t.Fatal(err)
	}
	mustNoLegacy("remove")
}
