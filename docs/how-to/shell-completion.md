# Set up shell completion

Generate a shell completion script so `gskill` commands tab-complete in your shell. Bash, Zsh, and
Fish are supported.

## Before you start

- GSKILL installed and on your `PATH`.

## Generate the script

```bash
gskill completion bash      # prints a bash completion script
gskill completion zsh       # prints a zsh completion script
gskill completion fish      # prints a fish completion script
```

`gskill completion` writes the script to **stdout** — redirect or source it for your shell.

## Install it

**Bash** (Linux):

```bash
gskill completion bash | sudo tee /etc/bash_completion.d/gskill > /dev/null
```

**Bash** (macOS, with Homebrew bash-completion):

```bash
gskill completion bash > "$(brew --prefix)/etc/bash_completion.d/gskill"
```

**Zsh** — place it on your `fpath`:

```bash
gskill completion zsh > "${fpath[1]}/_gskill"
```

**Fish**:

```bash
gskill completion fish > ~/.config/fish/completions/gskill.fish
```

Open a new shell afterwards. On Windows, use your shell's completion mechanism (e.g. Git Bash for the
bash script).

## Expected result

- The chosen shell tab-completes `gskill` subcommands. `gskill completion` itself exits `0`.

## See also

- [Command reference](../reference/commands.md)
