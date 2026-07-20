package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// hasLine reports whether data contains pattern as a whole trimmed line.
func hasLine(data, pattern string) bool {
	for _, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == pattern {
			return true
		}
	}
	return false
}

func TestInit_GitignoresStoreAndActiveLayer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := app.New(app.Options{})

	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, pattern := range []string{".gskill/", ".agents/"} {
		if !hasLine(string(data), pattern) {
			t.Errorf(".gitignore missing %q\n--- content ---\n%s", pattern, data)
		}
	}
}

func TestInit_GitignoreIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := app.New(app.Options{})

	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("Init not idempotent on .gitignore:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	// Exactly one occurrence of each pattern.
	for _, pattern := range []string{".gskill/", ".agents/"} {
		if n := strings.Count(string(second), pattern); n != 1 {
			t.Errorf("pattern %q appears %d times, want 1", pattern, n)
		}
	}
}

// TestEnsureGitignore_PreservesExistingFileMode (spec 017 FR-005/T005): Init
// must preserve an existing .gitignore's permission bits rather than forcing
// them to whatever mode ensureGitignore's atomic rewrite happens to use.
func TestEnsureGitignore_PreservesExistingFileMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []os.FileMode{0o644, 0o600} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			path := filepath.Join(root, ".gitignore")
			if err := os.WriteFile(path, []byte("node_modules/\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}

			a := app.New(app.Options{})
			if _, err := a.Init(context.Background(), root, false); err != nil {
				t.Fatalf("Init: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != mode {
				t.Errorf(".gitignore mode = %v, want %v (preserved from before Init)", got, mode)
			}
		})
	}
}

// TestInit_GitignorePreservesCustomEntriesAndOrder (spec 017 FR-005/T006):
// existing lines survive untouched, in order, and a substring look-alike
// (.agents/skills) must not be mistaken for the whole-line .agents/ pattern.
func TestInit_GitignorePreservesCustomEntriesAndOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	custom := "node_modules/\ndist/\n*.log\n.agents/skills\n"
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}

	a := app.New(app.Options{})
	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	wantPrefix := []string{"node_modules/", "dist/", "*.log", ".agents/skills"}
	for i, want := range wantPrefix {
		if i >= len(lines) || lines[i] != want {
			t.Fatalf("line %d = %q, want %q (custom entries must survive untouched and in order)\n--- content ---\n%s", i, safeLine(lines, i), want, data)
		}
	}
	for _, pattern := range []string{".gskill/", ".agents/"} {
		if !hasLine(string(data), pattern) {
			t.Errorf(".gitignore missing %q despite .agents/skills being present (substring must not satisfy whole-line match)\n--- content ---\n%s", pattern, data)
		}
	}
}

func safeLine(lines []string, i int) string {
	if i >= len(lines) {
		return "<missing>"
	}
	return lines[i]
}

// TestInit_GitignoreNoTrailingNewline (spec 017 FR-005 edge case/T007): an
// existing .gitignore with no trailing newline must not have the appended
// pattern concatenated onto its last line.
func TestInit_GitignoreNoTrailingNewline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(path, []byte("dist/"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := app.New(app.Options{})
	if _, err := a.Init(context.Background(), root, false); err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // test reads a file in its own temp dir
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for _, l := range lines {
		if l == "dist/" {
			found = true
		}
		if strings.Contains(l, "dist/") && l != "dist/" {
			t.Errorf("dist/ concatenated with another pattern: %q\n--- content ---\n%s", l, data)
		}
	}
	if !found {
		t.Errorf("dist/ missing as its own line:\n--- content ---\n%s", data)
	}
	for _, pattern := range []string{".gskill/", ".agents/"} {
		if !hasLine(string(data), pattern) {
			t.Errorf(".gitignore missing %q\n--- content ---\n%s", pattern, data)
		}
	}
}

// TestGitignore_CoversProjectState (spec 015 FR-014, T054): the managed
// .gskill/ ignore block covers the machine-local state.json, so it can never
// be committed.
func TestGitignore_CoversProjectState(t *testing.T) {
	t.Parallel()
	repo, ha, hb := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, ha, hb)
	if _, err := runLockInstall(t, root); err != nil {
		t.Fatal(err)
	}

	gi, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf(".gitignore missing after install: %v", err)
	}
	if !strings.Contains(string(gi), ".gskill/") {
		t.Fatalf(".gitignore lacks the .gskill/ entry that covers state.json:\n%s", gi)
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "state.json")); err != nil {
		t.Fatalf("state.json not written: %v", err)
	}
}
