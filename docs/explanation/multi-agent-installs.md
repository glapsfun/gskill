# Multi-agent installs

A single skill can be installed into several AI agents at once, and into either a project or your
user-global location. This page explains the model so you can predict where content lands.

## One skill, many agents

GSKILL treats each agent (Claude Code, Codex, Cursor, Antigravity CLI, OpenCode, OpenClaw, Hermes
Agent) as a target. When you install a skill, you choose which agents receive it:

- Pass `--agent <id>` one or more times to target specific agents.
- Pass nothing and GSKILL installs into the agents it **detects** in the project (by their markers:
  `.claude/`, `.codex/`, `.cursor/`, `.opencode/` or `opencode.json(c)`, `.hermes/`).

The lockfile records the full set of target agents and the exact path the skill was installed to for
each one, so a restore reproduces the same multi-agent layout everywhere.

## Shared-location agents

Antigravity CLI, OpenCode, OpenClaw, and Hermes Agent read project skills straight from
`.agents/skills/`, where GSKILL commits the content. For them the committed copy *is* the install: the
lockfile records the target with mode `shared`, and nothing else is written.

The consequence is that targeting cannot hide a project skill from these four agents. A skill you
install only for `claude` still sits in `.agents/skills/`, so all four can load it. Use targeting to
control what GSKILL records and installs globally, not what a shared-location agent can see.

## Why detection matters

Detection makes the common case effortless: if your project already uses Claude Code and Codex, a plain
`gskill add ./skill` installs into both without you listing them. But it also means a project with **no**
detected agents and no explicit `--agent` has nowhere to install — GSKILL writes nothing and exits `9`
rather than guessing.

## Project vs global scope

- **Project scope** (default) keeps skills with the project: content committed at
  `.agents/skills/<name>/`, agent directories linking into it. This is what you commit and
  reproduce per repository.
- **Agent-global installs** (`--global`) write verified copies into each agent's own user-global
  location, shared across projects. They sit outside the repo-owned model, backed by the same
  clone cache.

The lockfile records the agents and install mode, so intent and reality stay aligned.

## See also

- [Target specific agents](../how-to/target-specific-agents.md)
- [Supported agents](../reference/agents.md)
- [Repo-owned storage](repo-owned-storage.md)
