package cli

import (
	"fmt"
	"sort"
	"strings"
)

// completionCmd prints a shell completion script.
type completionCmd struct {
	Shell string `arg:"" enum:"bash,zsh,fish" help:"Shell: bash, zsh, or fish."`
}

// Help returns the detailed help shown by `gskill completion --help`.
func (completionCmd) Help() string {
	return examplesHelp(
		"gskill completion bash",
		`eval "$(gskill completion zsh)"`,
	)
}

// Run prints the completion script for the requested shell.
func (c completionCmd) Run(out *Output) error {
	script, err := completionScript(c.Shell)
	if err != nil {
		return err
	}
	return out.Result(script, map[string]any{"shell": c.Shell, "script": script})
}

// completionNames derives the words offered for completion from the live Kong
// grammar — every visible top-level command and every group's visible
// subcommands — plus every alias old name from aliasTable, so completion can
// never drift from the real command surface.
func completionNames() (string, error) {
	model, err := DocsModel()
	if err != nil {
		return "", fmt.Errorf("build completion model: %w", err)
	}

	seen := map[string]bool{}
	words := make([]string, 0, 32)
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			words = append(words, name)
		}
	}
	for _, node := range model.Children {
		if node.Hidden {
			continue
		}
		add(node.Name)
		for _, sub := range node.Children {
			if !sub.Hidden {
				add(sub.Name)
			}
		}
	}
	for _, m := range aliasTable {
		if m.Kind == aliasKindCommand {
			add(m.Old)
		}
	}
	sort.Strings(words)
	return strings.Join(words, " "), nil
}

// completionScript returns a minimal completion script listing top-level
// commands, project subcommands, and alias names.
func completionScript(shell string) (string, error) {
	names, err := completionNames()
	if err != nil {
		return "", err
	}
	switch shell {
	case "bash":
		return fmt.Sprintf("complete -W %q gskill\n", names), nil
	case "zsh":
		return fmt.Sprintf("#compdef gskill\n_gskill() { compadd %s }\ncompdef _gskill gskill\n", names), nil
	case "fish":
		return fmt.Sprintf("complete -c gskill -f -a %q\n", names), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}
