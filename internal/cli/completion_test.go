package cli

import (
	"regexp"
	"strings"
	"testing"
)

// completionWords is everything shell completion must offer, and nothing
// more: every visible canonical command, every visible subcommand of the
// three groups, and every alias old name (contract §5/§8). The lists are
// spelled out on purpose — they lock the contract independently of the
// grammar the production code derives from. `upgrade` was absent here from
// spec 024 until spec 025 precisely because the assertion was containment
// rather than equality; it is set equality now.
func completionWords() []string {
	words := []string{
		"add", "onboard", "install", "update", "upgrade", "remove",
		"list", "info", "search", "outdated",
		"project", "sync", "repair", "verify", "check",
		"cache", "stats", "clean",
		"config", "get",
		"doctor", "dashboard", "completion", "version",
	}
	for _, m := range aliasTable {
		if m.Kind == aliasKindCommand {
			words = append(words, m.Old)
		}
	}
	return words
}

// completionScriptWords extracts the offered word set from a completion
// script. bash and fish embed the list as a single quoted string; zsh passes
// it to compadd unquoted.
func completionScriptWords(t *testing.T, shell, script string) map[string]bool {
	t.Helper()

	var list string
	switch shell {
	case "zsh":
		const open = "compadd "
		start := strings.Index(script, open)
		end := strings.Index(script, " }")
		if start < 0 || end <= start {
			t.Fatalf("zsh completion script has no compadd word list:\n%s", script)
		}
		list = script[start+len(open) : end]
	default:
		first := strings.Index(script, `"`)
		last := strings.LastIndex(script, `"`)
		if first < 0 || last <= first {
			t.Fatalf("%s completion script has no quoted word list:\n%s", shell, script)
		}
		list = script[first+1 : last]
	}
	got := make(map[string]bool)
	for _, w := range strings.Fields(list) {
		got[w] = true
	}
	return got
}

func TestCompletion_CoversCanonicalCommandsAndAliases(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, code := runCLI(t, nil, "completion", shell)
			if code != 0 {
				t.Fatalf("completion %s: exit code = %d, stderr: %q", shell, code, stderr)
			}
			// Set equality (spec 025 FR-015): a word that should have been
			// removed and one that was never added must both fail.
			got := completionScriptWords(t, shell, stdout)
			want := completionWords()
			wantSet := make(map[string]bool, len(want))
			for _, w := range want {
				wantSet[w] = true
				if !got[w] {
					t.Errorf("completion %s missing %q", shell, w)
				}
			}
			for w := range got {
				if !wantSet[w] {
					t.Errorf("completion %s offers unexpected word %q", shell, w)
				}
			}
		})
	}
}

// retiredCommandNames are words that must never be offered by shell
// completion, because no command anywhere in the grammar spells them any
// more: `diff` (spec 025) plus the commands retired by specs 020, 021, and
// 022. The completion word list is flat and position-independent, so
// `sync`, `repair`, `verify`, and `check` deliberately remain — they are the
// leaves of `project`, and spec 025 retired only their top-level aliases.
// That the *top level* rejects them is locked by
// TestFlatMaintenanceAliasesRemoved; that the word set is exactly right is
// locked by TestCompletion_CoversCanonicalCommandsAndAliases.
var retiredCommandNames = []string{
	"diff", "status", "init", "source", "unlink", "store",
}

// TestCompletion_OmitsRetiredCommands is spec 025 FR-003: a retired name may
// not reach a user's shell, in any of the three supported shells. Matching is
// word-boundary anchored because the completion script embeds the word list
// inside a single quoted string.
func TestCompletion_OmitsRetiredCommands(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, code := runCLI(t, nil, "completion", shell)
			if code != 0 {
				t.Fatalf("completion %s: exit code = %d, stderr: %q", shell, code, stderr)
			}
			for _, name := range retiredCommandNames {
				re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
				if re.MatchString(stdout) {
					t.Errorf("completion %s offers retired command %q:\n%s", shell, name, stdout)
				}
			}
		})
	}
}
