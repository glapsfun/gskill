# Multi-agent installs

A single skill can be installed into several AI agents at once, and into either a project or your
user-global location. This page explains the model so you can predict where content lands.

## One skill, many agents

GSKILL treats each agent (Claude Code, Codex, Cursor, Gemini CLI) as a target with its own skills
directory. When you install a skill, you choose which agents receive it:

- Pass `--agent <id>` one or more times to target specific agents.
- Pass nothing and GSKILL installs into the agents it **detects** in the project (by their marker
  directories: `.claude/`, `.codex/`, `.cursor/`, `.gemini/`).

The lockfile records the full set of target agents and the exact path the skill was installed to for
each one, so a restore reproduces the same multi-agent layout everywhere.

## Why detection matters

Detection makes the common case effortless: if your project already uses Claude Code and Codex, a plain
`gskill add ./skill` installs into both without you listing them. But it also means a project with **no**
detected agents and no explicit `--agent` has nowhere to install — GSKILL writes nothing and exits `9`
rather than guessing.

## Project vs global scope

- **Project scope** (default) keeps skills with the project, under each agent's project directory. This
  is what you commit and reproduce per repository.
- **Global scope** (`--global`) installs into your user-global location for each agent, shared across
  projects. Global paths follow platform conventions.

Scope is recorded in the lockfile alongside the agents and install mode, so intent and reality stay
aligned.

## See also

- [Target specific agents](../how-to/target-specific-agents.md)
- [Supported agents](../reference/agents.md)
- [The store and the cache](store-and-cache.md)
