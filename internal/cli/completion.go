package cli

import "fmt"

// commandNames are the top-level commands offered for shell completion.
const commandNames = "init add install remove update lock sync verify check " +
	"outdated list info diff doctor cache config completion tui version"

// completionCmd prints a shell completion script.
type completionCmd struct {
	Shell string `arg:"" enum:"bash,zsh,fish" help:"Shell: bash, zsh, or fish."`
}

// Run prints the completion script for the requested shell.
func (c completionCmd) Run(out *Output) error {
	script, err := completionScript(c.Shell)
	if err != nil {
		return err
	}
	return out.Result(script, map[string]any{"shell": c.Shell, "script": script})
}

// completionScript returns a minimal completion script listing top-level commands.
func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return fmt.Sprintf("complete -W %q gskill\n", commandNames), nil
	case "zsh":
		return fmt.Sprintf("#compdef gskill\n_gskill() { compadd %s }\ncompdef _gskill gskill\n", commandNames), nil
	case "fish":
		return fmt.Sprintf("complete -c gskill -f -a %q\n", commandNames), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}
