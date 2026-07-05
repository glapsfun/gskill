package cli

import (
	"strings"
	"testing"
)

// completionWords is everything shell completion must offer: every visible
// canonical command, every project subcommand, and every alias old name
// (contract §8). The command list is spelled out on purpose — it locks the
// contract independently of the grammar the production code derives from.
func completionWords() []string {
	words := []string{
		"init", "add", "install", "update", "remove",
		"list", "status", "info", "search", "outdated",
		"project",
		"source", "cache", "config", "unlink", "doctor", "dashboard", "completion", "version",
	}
	for _, m := range aliasTable {
		if m.Kind == aliasKindCommand {
			words = append(words, m.Old)
		}
	}
	return words
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
			for _, word := range completionWords() {
				if !strings.Contains(stdout, word) {
					t.Errorf("completion %s missing %q", shell, word)
				}
			}
		})
	}
}
