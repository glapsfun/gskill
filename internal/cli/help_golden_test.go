package cli_test

import (
	"regexp"
	"sort"
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

	{"add", []string{"add"}},
	{"onboard", []string{"onboard"}},
	{"install", []string{"install"}},
	{"update", []string{"update"}},
	{"upgrade", []string{"upgrade"}},
	{"remove", []string{"remove"}},

	{"list", []string{"list"}},
	{"info", []string{"info"}},
	{"search", []string{"search"}},
	{"outdated", []string{"outdated"}},

	{"project-sync", []string{"project", "sync"}},
	{"project-repair", []string{"project", "repair"}},
	{"project-verify", []string{"project", "verify"}},
	{"project-check", []string{"project", "check"}},

	{"cache-stats", []string{"cache", "stats"}},
	{"cache-list", []string{"cache", "list"}},
	{"cache-clean", []string{"cache", "clean"}},

	{"config-list", []string{"config", "list"}},
	{"config-get", []string{"config", "get"}},
	{"doctor", []string{"doctor"}},
	{"dashboard", []string{"dashboard"}},
	{"completion", []string{"completion"}},
	{"version", []string{"version"}},
}

// visibleTopLevel is the canonical 17-entry command surface (spec 010 FR-001
// + spec 011 onboard + spec 024 upgrade, minus the commands retired by spec
// 021, the store/migrate/projects trees retired by spec 022, and the hidden
// flat aliases retired by spec 025). It is spelled out on purpose, so it
// locks the contract independently of the grammar the help is rendered from,
// and it is compared by set EQUALITY: a containment check cannot notice a
// command that was added and never registered here — which is exactly how
// `upgrade` went missing from this list between spec 024 and spec 025.
var visibleTopLevel = []string{
	"add", "onboard", "install", "update", "upgrade", "remove",
	"list", "info", "search", "outdated",
	"project",
	"cache", "config", "doctor",
	"dashboard", "completion", "version",
}

// topLevelEntryRE matches one command entry in the root help: a name at
// exactly two spaces of indentation. Descriptions sit at four spaces and the
// flag block's entries start with a dash, so neither is captured.
var topLevelEntryRE = regexp.MustCompile(`(?m)^ {2}([a-z][a-z0-9-]*)\b`)

// rootHelpCommands returns the set of command names the root help advertises.
func rootHelpCommands(stdout string) map[string]bool {
	got := make(map[string]bool)
	for _, m := range topLevelEntryRE.FindAllStringSubmatch(stdout, -1) {
		got[m[1]] = true
	}
	return got
}

// diffSets returns the names present in got but not want, and vice versa.
func diffSets(got map[string]bool, want []string) (extra, missing []string) {
	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
		if !got[n] {
			missing = append(missing, n)
		}
	}
	for n := range got {
		if !wantSet[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	return extra, missing
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
	// Set equality, not containment (spec 025 FR-015): the advertised surface
	// must be exactly the contracted inventory, so neither an unregistered new
	// command nor a stale leftover can pass unnoticed.
	extra, missing := diffSets(rootHelpCommands(stdout), visibleTopLevel)
	if len(missing) > 0 {
		t.Errorf("root help missing visible command(s) %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("root help advertises unexpected top-level command(s) %v", extra)
	}
	// The regrouped maintenance commands must not appear as top-level entries:
	// spec 010 moved them under `project`, and spec 025 removed the hidden
	// flat aliases that survived that move, so these spellings no longer exist
	// at all. `status` (spec 020) and `init`/`source`/`unlink` (spec 021) stay
	// in this list for the same reason — removed outright, and they must never
	// resurface in root help.
	for _, old := range []string{"sync", "repair", "lock", "verify", "check", "diff", "status", "init", "source", "unlink"} {
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
	for _, sub := range []string{"sync", "repair", "verify", "check"} {
		re := regexp.MustCompile(`(?m)^\s+project ` + sub + `\b`)
		if !re.MatchString(stdout) {
			t.Errorf("project group help missing subcommand %q", sub)
		}
	}
	if regexp.MustCompile(`(?m)^\s+project diff\b`).MatchString(stdout) {
		t.Errorf("project group help still lists the removed `diff` subcommand")
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
