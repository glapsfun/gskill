---
name: gskill
description: Manage AI agent skills with the GSKILL CLI (gskill). Use when the user asks to find, add, install, restore, list, inspect, update, upgrade, verify, troubleshoot, or remove skills, or mentions skills.toml or skills-lock.json.
license: MIT
compatibility: Requires the gskill CLI 0.12.0 or newer on Linux or macOS.
requires:
  commands: ["gskill"]
---

# Manage skills with GSKILL

GSKILL installs, versions, locks, and verifies `SKILL.md` skills for AI agents. You drive it
through the shell with the `gskill` command. Turn each request into explicit, non-interactive
commands, read their results, and report what changed.

## 1. Check the CLI

```sh
gskill --json version
```

- Not found, or older than 0.12.0: stop and use the `gskill-install` skill. Without it, point
  the user to https://glapsfun.github.io/gskill/how-to/use-gskill-from-an-agent/.
- A leading `v` is fine. A version with a suffix, such as
  `v0.12.1-0.20261112112558-afb0314d28b3+dirty`, is a build from source; treat it as supported
  and say so.
- If `gskill` works only by its absolute path (for example `"$HOME/.local/bin/gskill"`), use
  that path in every command below.

## 2. Establish the target

Before changing anything, know:

- **Project**: the directory holding `skills.toml` and `skills-lock.json`. Use `-C <dir>` when
  it is not the current directory.
- **Source and skills**: the source string the user gave (`github.com/owner/repo`, a URL, or a
  local path) and which skills from it. If the source has several skills and the request names
  none, list them (`add <source> --list`) and ask.
- **Agents**: the agents the user named, else the agent you are. Always pass `--agent`: without
  it GSKILL uses the agents it detects, and when it detects none it installs for `claude`.
- **Scope**: project unless the user asks for every project (`--global`).
- **Version policy**: newest release, a range (`--version '^1.2.0'`), a tag (`--ref`), or a
  commit (`--commit`), as the user said.

Read the current state with `gskill --no-interactive --json list` and, when it exists,
`skills.toml`. Ask one short question when a missing choice would change the result.

## 3. Run rules

- Always pass `--no-interactive`. Pass `--json` when you will read the result.
- Never run `gskill dashboard`, `gskill tui`, or `gskill onboard`; they need a person at a
  terminal. Name the skills, or use the documented flags, so no picker is needed.
- Pass `--yes` or `--force` only for an operation the user asked for. `remove` needs `--force`
  without a terminal.
- Never accept changed upstream content (`install --force`), discard local edits
  (`project repair`), or prune installs (`--prune`) without the user's explicit approval.
- Do not edit `skills-lock.json` or files under `.agents/skills/`. The one file you may edit is
  `skills.toml`, and only as described in the workflows.

## 4. Pick the command

| Request | Command | Changes |
| --- | --- | --- |
| find skills | `gskill --no-interactive --json search kubernetes --owner acme` | nothing |
| list a source's skills | `gskill --no-interactive --json add github.com/acme/skills --list` | nothing |
| add a skill | `gskill --no-interactive --json add github.com/acme/skills --skill kubernetes --agent claude` | declaration, lock, content |
| change a skill's agents | edit `agents` in `skills.toml`, then `gskill --no-interactive --json install` | declaration, lock, links |
| install what is declared | `gskill --no-interactive --json install` | lock, content |
| restore exactly | `gskill --no-interactive --json install --frozen-lockfile` | content only |
| what is installed | `gskill --no-interactive --json list` | nothing |
| one skill's details | `gskill --no-interactive --json info kubernetes` | nothing |
| what can update | `gskill --no-interactive --json update --list` | nothing |
| apply updates | `gskill --no-interactive --json update kubernetes` | lock, content |
| move a version | `gskill --no-interactive --json upgrade kubernetes --to 2.1.0` | declaration, lock, content |
| check integrity | `gskill --no-interactive --json project verify` | nothing |
| check drift | `gskill --no-interactive --json project check` | nothing |
| check the environment | `gskill --no-interactive doctor` | nothing |
| remove | `gskill --no-interactive --json remove kubernetes --force` | declaration, lock, content |

"Declaration" is `skills.toml`, "lock" is `skills-lock.json`, "content" is
`.agents/skills/<name>/` and its agent links. Details, flags, and JSON fields for each row are
in [workflows](references/workflows.md).

## 5. Keep version intent

- `update` moves a skill only within what `skills.toml` declares and never edits it. A pinned
  skill is reported as pinned, with the command that would move it.
- `upgrade` changes the declared version. Run it only when the user asks for a different
  version, and say that `skills.toml` changes.
- Keep the user's exact pins and ranges. Do not widen a constraint on your own.

## 6. Read the result

Check the exit code first, then the JSON. Every code, what it means, and the next step to give
are in [results](references/results.md). Exit 0 with nothing changed is a no-op: say the skills
were already current. On a non-zero exit, quote stderr and do not retry with force flags.

## 7. Verify and report

After any change, confirm it:

```sh
gskill --no-interactive --json project verify
gskill --no-interactive --json list
```

Then report:

- what changed, per skill (name, version before and after, agents);
- which files changed (`skills.toml`, `skills-lock.json`, `.agents/skills/<name>/`, agent links)
  and that a project install should be committed;
- anything that failed, with its next step;
- follow-ups such as `hermes skills trust` after a project install for Hermes Agent.
