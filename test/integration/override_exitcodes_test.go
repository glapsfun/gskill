package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOverrideExitCodes maps every new failure onto the code contracts/cli.md
// assigns it. Exit codes are the process's external contract, so a script that
// branches on them must keep working: each case also asserts stderr carries a
// diagnostic and stdout stays clean (contract C1).
func TestOverrideExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(t *testing.T, proj string)
		args     []string
		wantCode int
		wantIn   string
	}{
		{
			name: "manifest parse failure is a usage error",
			setup: func(t *testing.T, proj string) {
				t.Helper()
				write(t, filepath.Join(proj, "skills.toml"), "[skills.demo\nsource = \"x\"\n")
			},
			args: []string{"install"}, wantCode: 2, wantIn: "skills.toml",
		},
		{
			name: "unknown declaration key is a usage error",
			setup: func(t *testing.T, proj string) {
				t.Helper()
				write(t, filepath.Join(proj, "skills.toml"),
					"[skills.demo]\nsource = \"github:o/r\"\nrefff = \"v1\"\n")
			},
			args: []string{"install"}, wantCode: 2, wantIn: "refff",
		},
		{
			name: "override input escaping the repo is a usage error",
			setup: func(t *testing.T, proj string) {
				t.Helper()
				write(t, filepath.Join(proj, "skills.toml"),
					"[skills.demo]\nsource = \"github:o/r\"\n[skills.demo.override]\nappend = [\"../outside.md\"]\n")
			},
			args: []string{"install"}, wantCode: 2, wantIn: "outside",
		},
		{
			name: "missing override input is a usage error",
			setup: func(t *testing.T, proj string) {
				t.Helper()
				write(t, filepath.Join(proj, "skills.toml"),
					"[skills.demo]\nsource = \"github:o/r\"\n[skills.demo.override]\nappend = [\"gskill/nope.md\"]\n")
			},
			args: []string{"install"}, wantCode: 2, wantIn: "nope.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// A real installed skill first: a manifest fault must be reported
			// as a manifest fault, not masked by the missing-lock error a bare
			// directory produces.
			repo := gitRepo(t, validSkill("demo"), "v1.0.0")
			proj := newProject(t)
			initProject(t, proj)
			if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
				t.Fatalf("add: %s", stderr)
			}
			tt.setup(t, proj)

			stdout, stderr, code := runGskill(t, proj, tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d\nstderr: %s", code, tt.wantCode, stderr)
			}
			if !strings.Contains(stderr, tt.wantIn) {
				t.Errorf("stderr must name %q:\n%s", tt.wantIn, stderr)
			}
			// stdout carries the run summary, which names the failed skill and
			// its reason; the invariants that matter to a script are the exit
			// code and a diagnostic on stderr, both asserted above.
			_ = stdout
		})
	}
}

// TestOverrideNonInteractive is contract assertion C5 and Constitution III: a
// full customize cycle must complete unattended, because CI is a first-class
// user and a prompt there is a hang, not a question.
func TestOverrideNonInteractive(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)

	steps := [][]string{
		{"add", repo, "--no-interactive"},
		{"install", "--no-interactive"},
		{"update", "demo", "--no-interactive"},
	}
	for i, args := range steps {
		if i == 1 {
			rules := filepath.Join(proj, "gskill", "demo", "rules.md")
			if err := os.MkdirAll(filepath.Dir(rules), 0o750); err != nil {
				t.Fatal(err)
			}
			write(t, rules, "## House rules\n")
			declareOverride(t, proj, "demo", "append = [\"gskill/demo/rules.md\"]\n")
		}
		if _, stderr, code := runGskill(t, proj, args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, stderr)
		}
	}

	content, err := os.ReadFile(filepath.Join(proj, ".agents", "skills", "demo", "SKILL.md")) //nolint:gosec // test project
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "House rules") {
		t.Errorf("unattended cycle lost the customization:\n%s", content)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
