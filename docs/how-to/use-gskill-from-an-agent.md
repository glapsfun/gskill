# Use GSKILL from an AI agent

GSKILL publishes two skills for AI agents. `gskill-install` installs and checks the `gskill`
CLI, and works before GSKILL is on the machine. `gskill` teaches the agent to manage skills with
the CLI. With both loaded, you ask in plain words and the agent runs the commands.

## Before you start

- Linux or macOS on amd64 or arm64.
- An agent GSKILL supports (see [Supported agents](../reference/agents.md)) that can run shell
  commands.
- `curl` and `tar`, to load the setup skill before GSKILL is installed.

## Load the setup skill

`gskill-install` must be in place before GSKILL is, so you copy it by hand from the release
archive. Put it in your agent's global skills folder, because installing a CLI is a
per-machine task. For Claude Code:

```sh
dest="$HOME/.claude/skills"
mkdir -p "$dest"
curl -sSfL https://github.com/glapsfun/gskill/archive/refs/tags/v0.13.0.tar.gz | tar -xzf - -C "$dest" --strip-components=2 gskill-0.13.0/skills/gskill-install
```

This writes only `gskill-install/` (its `SKILL.md` and `references/`). For another agent, set
`dest` to its folder:

| Agent | Global folder | Project folder | After copying |
| --- | --- | --- | --- |
| Claude Code | `~/.claude/skills` | `.claude/skills` | Start a new session. |
| Codex | `~/.codex/skills` | `.codex/skills` | Start a new session. |
| Cursor | `~/.cursor/skills` | `.cursor/skills` | Start a new session. |
| Antigravity CLI | `~/.gemini/antigravity-cli/skills` | `.agents/skills` | Start a new session. |
| OpenCode | `~/.config/opencode/skills` | `.agents/skills` | Start a new session. |
| OpenClaw | `~/.openclaw/skills` | `.agents/skills` | Project skills load only when the repository is the OpenClaw workspace. |
| Hermes Agent | `~/.hermes/skills` | `.agents/skills` | For a project folder, run `hermes skills trust` once. |

Then ask your agent:

> Set up GSKILL.

The agent checks for an existing `gskill`, checks your platform, proposes an install method,
and waits for your approval before it installs anything. It then runs `gskill version` to prove
the install worked.

GSKILL does not manage a hand-copied `gskill-install`. To let GSKILL manage it later, delete the
copied folder first, then install it with the command in the next section. GSKILL refuses to
overwrite a folder it does not manage.

## Install the usage skill

Once GSKILL works, install the `gskill` skill with GSKILL itself. The setup skill offers this
step; you can also run it yourself. Each command selects only the official skills from the
GSKILL repository and pins them to a release:

| Goal | Command |
| --- | --- |
| Usage skill, this project | `gskill add github.com/glapsfun/gskill/skills/gskill --ref v0.13.0 --agent claude` |
| Usage skill, every project | `gskill add github.com/glapsfun/gskill/skills/gskill --ref v0.13.0 --agent claude --global` |
| Setup skill only | `gskill add github.com/glapsfun/gskill/skills/gskill-install --ref v0.13.0 --agent claude` |
| Both skills | `gskill add github.com/glapsfun/gskill --include 'skills/*' --all --ref v0.13.0 --agent claude` |
| Move to the newest release | `gskill upgrade gskill --latest` |

Replace `claude` with any agent ID from [Supported agents](../reference/agents.md). `--agent`
is repeatable and takes a comma-separated list.

The source path matters. The GSKILL repository also holds its own development skills, a docs
example, and test fixtures; `skills/gskill` or `--include 'skills/*'` keeps all of those out.

For a project install, commit `skills.toml`, `skills-lock.json`, `.agents/skills/gskill/`, and
the agent link (for example `.claude/skills/gskill`), so teammates get the skill on clone.

A global install puts the skill in the agent's user-global folder. GSKILL still records it in
the `skills.toml` and `skills-lock.json` of the directory you ran the command in, so run it from
a project you keep.

## Example sessions

Each example shows what you type, the command the agent runs, and what it reports. The full
list of requests the usage skill handles is in its
[workflows reference](../../skills/gskill/references/workflows.md).

### Add a skill

> Add the hello-gskill skill from github.com/glapsfun/gskill/examples/skills/hello-gskill for Claude.

```sh
gskill --no-interactive --json add github.com/glapsfun/gskill/examples/skills/hello-gskill --agent claude
```

The agent reports the version it installed, the agent link it created
(`.claude/skills/hello-gskill`), and the files to commit: `skills.toml`, `skills-lock.json`,
`.agents/skills/hello-gskill/`, and the link.

### See what is installed

> Which skills does this project have?

```sh
gskill --no-interactive --json list
```

The agent lists each skill with its version, agents, and health.

### Check for updates

> Are any of my skills out of date?

```sh
gskill --no-interactive --json update --list
```

The agent reports each skill's current and available version. A pinned skill is reported as
pinned, with the command that would move it; the agent does not move it on its own.

### Change a version

> Move hello-gskill to the latest release.

```sh
gskill --no-interactive --json upgrade hello-gskill --latest
```

The agent reports the version before and after, and that `skills.toml` changed along with the
lockfile and the installed content.

### Verify

> Check that no skill was tampered with.

```sh
gskill --no-interactive --json project verify
```

The agent reports that every skill matches the lockfile, or names the ones that do not. It does
not repair them without asking, because repairing discards local edits.

### Remove a skill

> Remove hello-gskill.

```sh
gskill --no-interactive --json remove hello-gskill --force
```

`--force` is needed without a terminal. The agent passes it only because you asked for the
removal, then reports what was removed.

### Ask for a newer GSKILL

> Update GSKILL to the latest version.

The setup skill first asks GSKILL how it was installed. Nothing changes in a dry run:

```sh
gskill --json --dry-run self upgrade
```

- Installed with the install script: after you approve, the agent runs
  `gskill --yes self upgrade` and reports the new version.
- Installed with Homebrew, npm, Go, or the container image: the agent shows the upgrade command
  for that tool (for example `brew upgrade glapsfun/tap/gskill`) and runs it only after you
  approve. It never installs a second copy another way.

## What the skills do on their own

- **Ask before changing your machine.** The setup skill asks before it installs GSKILL, before
  it uses `sudo`, and before it edits a shell profile, unless your request already said so.
- **Never open a terminal UI.** The usage skill runs every command with `--no-interactive` and
  never starts `gskill dashboard` or `gskill onboard`.
- **Use force flags only for what you asked.** `--yes` and `--force` appear only for an
  operation you requested, such as `remove --force` after "remove this skill". The skill never
  accepts changed upstream content, repairs over local edits, or prunes installs without asking.
- **Keep your version intent.** `update` stays within what `skills.toml` declares. The skill
  changes a declared version with `upgrade` only when you ask for another version.
- **Stay in project scope.** Installs go into the current project unless you ask for every
  project.
- **Verify, then report.** After a change the agent runs `gskill project verify` or
  `gskill list` and tells you what changed, what failed, and what to commit.

## Requirements metadata

The usage skill declares the `gskill` command in its frontmatter:

```yaml
requires:
  commands: ["gskill"]
```

GSKILL records this and `gskill doctor` checks it. It installs nothing: if `gskill` is missing,
the usage skill sends you to the setup skill. The setup skill declares no requirements, so it
loads on a machine without GSKILL.

## Supported versions

- Both skills need GSKILL 0.12.0 or newer.
- A skill's version is the GSKILL release tag it was installed from, `v0.13.0` in the commands
  above. `gskill update` reports a tag pin as pinned; move it with
  `gskill upgrade gskill --latest`.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| The agent does not use the skill | Start a new agent session. For Hermes Agent, run `hermes skills trust` in the project. For OpenClaw, open the repository as its workspace or install with `--global`. |
| `gskill: command not found` after setup | The install directory is not on `PATH`. The agent keeps using the full path; add the directory to `PATH` in your shell profile for new shells. |
| `unsupported OS` or `unsupported architecture` | GSKILL runs on Linux and macOS, amd64 and arm64 only. |
| `download failed` | The machine cannot reach GitHub. Restore network access or set a proxy. |
| `permission denied` writing `/usr/local/bin` | Install to your own directory: the agent sets `INSTALL_DIR="$HOME/.local/bin"`. |
| Two `gskill` versions on `PATH` | Keep one, and remove the other with the tool that installed it. `which -a gskill` lists both. |
| Installing a skill fails with "not managed by gskill" | A hand-copied skill is in the way. Delete the copy, then run the install command again. |
| Exit code 6 after `gskill project verify` | Installed content was edited. `gskill project repair` restores it and discards the edits. |
| Exit code 9 | The agent ID is not one GSKILL supports. Use an ID from [Supported agents](../reference/agents.md). |

The setup skill's [troubleshooting reference](../../skills/gskill-install/references/troubleshooting.md)
and the usage skill's [results reference](../../skills/gskill/references/results.md) list every
case the skills handle.

## See also

- [Install GSKILL](../installation.md)
- [Supported agents](../reference/agents.md)
- [Script GSKILL with `--json`](script-with-json.md)
- [Exit codes](../reference/exit-codes.md)
- [The skills' source](../../skills/README.md)
