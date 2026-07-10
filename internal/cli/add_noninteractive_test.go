package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the SC-004 regression baseline for spec 011 (TUI onboarding):
// it locks in the NON-INTERACTIVE behavior of `gskill add` as it exists before
// the guided wizard lands, and must pass unchanged after every wizard task.
// runCLI drives the CLI with in-memory buffers, so stdout is never a TTY and
// every invocation here is inherently non-interactive. Note that --yes is NOT
// a flow suppressor (FR-004): on a TTY it only pre-answers approval; here it
// simply must not change non-interactive results.

// addSourceTree builds a local source directory with one valid skill per name.
func addSourceTree(t *testing.T, names ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, name := range names {
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// agentProject returns an inited project with a .claude marker dir so the
// default agent is detected.
func agentProject(t *testing.T) string {
	t.Helper()

	dir := initedProject(t)
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAddBaseline_NonInteractiveMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// project setup: "inited" (with agent marker) or "empty"
		empty      bool
		multiSkill bool // source has 3 skills instead of 1
		args       []string
		wantCode   int
		wantOut    string // substring of stdout ("" = don't check)
		wantErrSub string // substring of stderr ("" = don't check)
		wantJSON   bool   // stdout must be valid JSON
	}{
		{
			name:       "multi-skill source without selector fails with usage guidance",
			multiSkill: true,
			args:       []string{"add", "SRC"},
			wantCode:   2,
		},
		{
			name:     "explicit --skill installs that skill",
			args:     []string{"add", "SRC", "--skill", "alpha"},
			wantCode: 0,
			wantOut:  "Installed 1 skill(s): alpha",
		},
		{
			name:       "--all installs every valid skill",
			multiSkill: true,
			args:       []string{"add", "SRC", "--all"},
			wantCode:   0,
			wantOut:    "Installed 3 skill(s)",
		},
		{
			name:     "single-skill source auto-selects and installs directly",
			args:     []string{"add", "SRC"},
			wantCode: 0,
			wantOut:  "Installed 1 skill(s): alpha",
		},
		{
			name:     "--yes does not change non-interactive install",
			args:     []string{"--yes", "add", "SRC", "--skill", "alpha"},
			wantCode: 0,
			wantOut:  "Installed 1 skill(s): alpha",
		},
		{
			name:     "--no-interactive is explicit non-interactive",
			args:     []string{"--no-interactive", "add", "SRC", "--skill", "alpha"},
			wantCode: 0,
			wantOut:  "Installed 1 skill(s): alpha",
		},
		{
			name:     "--json emits machine-readable result",
			args:     []string{"--json", "add", "SRC", "--skill", "alpha"},
			wantCode: 0,
			wantJSON: true,
		},
		{
			name:       "--list is read-only listing",
			multiSkill: true,
			args:       []string{"add", "SRC", "--list"},
			wantCode:   0,
			wantOut:    "alpha",
		},
		{
			name:     "empty project installs without any manifest",
			empty:    true,
			args:     []string{"add", "SRC", "--skill", "alpha"},
			wantCode: 0,
			wantOut:  "Installed 1 skill(s): alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			skills := []string{"alpha"}
			if tt.multiSkill {
				skills = []string{"alpha", "beta", "gamma"}
			}
			src := addSourceTree(t, skills...)

			var dir string
			if tt.empty {
				dir = t.TempDir()
			} else {
				dir = agentProject(t)
			}

			args := make([]string, 0, len(tt.args)+2)
			args = append(args, "-C", dir)
			for _, a := range tt.args {
				if a == "SRC" {
					a = src
				}
				args = append(args, a)
			}

			stdout, stderr, code := runCLI(t, newTestApp(), args...)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d\nstdout: %q\nstderr: %q", code, tt.wantCode, stdout, stderr)
			}
			if tt.wantOut != "" && !strings.Contains(stdout, tt.wantOut) {
				t.Errorf("stdout = %q, want substring %q", stdout, tt.wantOut)
			}
			if tt.wantErrSub != "" && !strings.Contains(stderr, tt.wantErrSub) {
				t.Errorf("stderr = %q, want substring %q", stderr, tt.wantErrSub)
			}
			if tt.wantJSON {
				var v any
				if err := json.Unmarshal([]byte(stdout), &v); err != nil {
					t.Errorf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
				}
			}
		})
	}
}

// TestAddBaseline_ListWritesNothing pins the read-only guarantee of --list.
func TestAddBaseline_ListWritesNothing(t *testing.T) {
	t.Parallel()

	src := addSourceTree(t, "alpha", "beta")
	dir := agentProject(t)

	_, stderr, code := runCLI(t, newTestApp(), "-C", dir, "add", src, "--list")
	if code != 0 {
		t.Fatalf("add --list: exit code = %d, stderr: %q", code, stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, "skills-lock.json")); err == nil {
		t.Error("add --list created a lockfile")
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill.toml")); err == nil {
		t.Error("add --list created a manifest")
	}
}
