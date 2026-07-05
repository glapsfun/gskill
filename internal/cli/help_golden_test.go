package cli_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/testutil"
)

// helpPages enumerates every help page locked by a golden file: the root, the
// project group, and each runnable leaf command (canonical paths only).
var helpPages = []struct {
	name string   // golden file stem under testdata/help/
	args []string // command path (without --help)
}{
	{"root", nil},
	{"project", []string{"project"}},

	{"init", []string{"init"}},
	{"add", []string{"add"}},
	{"install", []string{"install"}},
	{"update", []string{"update"}},
	{"remove", []string{"remove"}},

	{"list", []string{"list"}},
	{"status", []string{"status"}},
	{"info", []string{"info"}},
	{"search", []string{"search"}},
	{"outdated", []string{"outdated"}},

	{"project-sync", []string{"project", "sync"}},
	{"project-repair", []string{"project", "repair"}},
	{"project-lock", []string{"project", "lock"}},
	{"project-verify", []string{"project", "verify"}},
	{"project-check", []string{"project", "check"}},
	{"project-diff", []string{"project", "diff"}},

	{"source-list", []string{"source", "list"}},
	{"source-inspect", []string{"source", "inspect"}},
	{"source-check", []string{"source", "check"}},
	{"cache-path", []string{"cache", "path"}},
	{"cache-stats", []string{"cache", "stats"}},
	{"cache-list", []string{"cache", "list"}},
	{"cache-clean", []string{"cache", "clean"}},
	{"config-path", []string{"config", "path"}},
	{"config-list", []string{"config", "list"}},
	{"config-get", []string{"config", "get"}},
	{"unlink", []string{"unlink"}},
	{"doctor", []string{"doctor"}},
	{"dashboard", []string{"dashboard"}},
	{"completion", []string{"completion"}},
	{"version", []string{"version"}},
}

// visibleTopLevel is the canonical 19-entry command surface (FR-001).
var visibleTopLevel = []string{
	"init", "add", "install", "update", "remove",
	"list", "status", "info", "search", "outdated",
	"project",
	"source", "cache", "config", "unlink", "doctor", "dashboard", "completion", "version",
}

// TestHelpPages_CoverEveryVisibleLeaf guards helpPages against drift: every
// visible runnable command in the live grammar must have a golden page (and
// thereby Examples enforcement), so a future command cannot ship without one.
func TestHelpPages_CoverEveryVisibleLeaf(t *testing.T) {
	t.Parallel()

	model, err := cli.DocsModel()
	if err != nil {
		t.Fatalf("DocsModel: %v", err)
	}

	covered := make(map[string]bool, len(helpPages))
	for _, page := range helpPages {
		covered[strings.Join(page.args, " ")] = true
	}

	for _, node := range model.Children {
		if node.Hidden {
			continue
		}
		if len(node.Children) == 0 {
			if !covered[node.Name] {
				t.Errorf("command %q has no helpPages entry (missing golden + Examples enforcement)", node.Name)
			}
			continue
		}
		for _, sub := range node.Children {
			if sub.Hidden {
				continue
			}
			path := node.Name + " " + sub.Name
			if !covered[path] {
				t.Errorf("command %q has no helpPages entry (missing golden + Examples enforcement)", path)
			}
		}
	}
}

func TestRootHelp_GroupedSections(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}

	for _, section := range []string{"CORE", "INSPECT", "PROJECT", "MORE"} {
		if !strings.Contains(stdout, section) {
			t.Errorf("root help missing section title %q", section)
		}
	}
	for _, name := range visibleTopLevel {
		re := regexp.MustCompile(`(?m)^\s+` + name + `\b`)
		if !re.MatchString(stdout) {
			t.Errorf("root help missing visible command %q", name)
		}
	}
	// The regrouped maintenance commands must not appear as top-level entries
	// (FR-004); they live under `project` and as hidden aliases only.
	for _, old := range []string{"sync", "repair", "lock", "verify", "check", "diff"} {
		re := regexp.MustCompile(`(?m)^\s{2,4}` + old + `\b`)
		if re.MatchString(stdout) {
			t.Errorf("root help lists hidden alias %q as a top-level entry", old)
		}
	}
	// Renames display the old name as a parenthesized annotation (FR-007).
	for _, ann := range []string{"search (find)", "dashboard (tui)"} {
		if !strings.Contains(stdout, ann) {
			t.Errorf("root help missing rename annotation %q", ann)
		}
	}
}

func TestProjectBareInvocation_ShowsGroupHelp(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := run(t, "project")
	if code != 0 {
		t.Fatalf("gskill project: exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	helpOut, _, helpCode := run(t, "project", "--help")
	if helpCode != 0 {
		t.Fatalf("gskill project --help: exit code = %d, want 0", helpCode)
	}
	if stdout != helpOut {
		t.Errorf("bare `gskill project` output differs from `gskill project --help`")
	}
	for _, sub := range []string{"sync", "repair", "lock", "verify", "check", "diff"} {
		re := regexp.MustCompile(`(?m)^\s+project ` + sub + `\b`)
		if !re.MatchString(stdout) {
			t.Errorf("project group help missing subcommand %q", sub)
		}
	}
}

func TestHelpGolden(t *testing.T) {
	t.Parallel()

	for _, page := range helpPages {
		t.Run(page.name, func(t *testing.T) {
			t.Parallel()

			args := append(append([]string(nil), page.args...), "--help")
			stdout, stderr, code := run(t, args...)
			if code != 0 {
				t.Fatalf("%v: exit code = %d, want 0 (stderr: %q)", args, code, stderr)
			}
			testutil.Golden(t, "help/"+page.name+".golden", []byte(stdout))
		})
	}
}

func TestEveryCommandHelp_HasUsageAndExamples(t *testing.T) {
	t.Parallel()

	for _, page := range helpPages {
		if page.name == "root" || page.name == "project" {
			continue // branch pages: usage locked by goldens, examples optional
		}
		t.Run(page.name, func(t *testing.T) {
			t.Parallel()

			args := append(append([]string(nil), page.args...), "--help")
			stdout, _, code := run(t, args...)
			if code != 0 {
				t.Fatalf("%v: exit code = %d, want 0", args, code)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("%v help missing Usage line", args)
			}
			if !strings.Contains(stdout, "Examples:") {
				t.Errorf("%v help missing Examples: block (FR-006)", args)
			}
		})
	}
}
