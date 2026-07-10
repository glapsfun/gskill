package app_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/skillslock"
)

func newInitApp() *app.App {
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// Init prepares local runtime state only: no manifest, no lock unless asked.
func TestInitCreatesDirsNotManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := newInitApp().Init(context.Background(), root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.toml")); !os.IsNotExist(err) {
		t.Fatal("init must not create gskill.toml")
	}
	for _, dir := range []string{".gskill", filepath.Join(".agents", "skills")} {
		if fi, err := os.Stat(filepath.Join(root, dir)); err != nil || !fi.IsDir() {
			t.Fatalf("missing dir %s: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, skillslock.FileName)); !os.IsNotExist(err) {
		t.Fatal("init without --lock must not create skills-lock.json")
	}
}

// Init --lock writes an empty shared lock, once.
func TestInitWithLockCreatesEmptyLock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := newInitApp().Init(context.Background(), root, true); err != nil {
		t.Fatal(err)
	}
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Names()) != 0 {
		t.Fatalf("expected empty lock, got %v", l.Names())
	}
}
