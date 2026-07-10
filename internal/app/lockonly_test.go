package app_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/skillslock"
)

func lockOnlyApp() *app.App {
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// Add on a bare directory creates only skills-lock.json — no manifest — and
// persists agents/mode intent in the entry's gskill block.
func TestAddCreatesLockOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	_, err := a.Add(context.Background(), app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"},
		Agents: []string{"claude", "codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "gskill.toml")); !os.IsNotExist(err) {
		t.Fatal("add must not create gskill.toml")
	}
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := l.Entry("demo-skill")
	if !ok || e.Ext == nil {
		t.Fatalf("lock entry missing or unenriched: %+v", e)
	}
	if got := e.Ext.Agents; !reflect.DeepEqual(got, []string{"claude", "codex"}) {
		t.Fatalf("agents not persisted: %v", got)
	}
	if e.ComputedHash == "" || e.Ext.StoreHash == "" {
		t.Fatal("integrity fields missing")
	}
}

// A second identical add fails as already-declared and leaves the lock
// byte-identical.
func TestAddIdempotencyGuard(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := sourceTree(t, "skills/demo-skill")
	a := lockOnlyApp()
	req := app.AddRequest{
		Root: root, Source: src, Selectors: []string{"demo-skill"},
		Agents: []string{"claude"},
	}
	if _, err := a.Add(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Add(context.Background(), req); err == nil {
		t.Fatal("second identical add must fail without --force")
	}
	after, err := os.ReadFile(filepath.Join(root, skillslock.FileName)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed add modified the lock")
	}
}
