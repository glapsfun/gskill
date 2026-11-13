# Supported agents

GSKILL installs skills into seven AI coding agents. Every project skill's content is committed once
at `.agents/skills/<name>/`. Claude Code, Codex, and Cursor each get their own skills directory that
links to it. Antigravity CLI, OpenCode, OpenClaw, and Hermes Agent read `.agents/skills/` directly,
so GSKILL creates nothing extra for them.

## Adapters

| Agent | Agent ID | Detected by | Project install | Global install (`--global`) |
| --- | --- | --- | --- | --- |
| Claude Code | `claude` | `.claude/` | `.claude/skills/<name>/` (link) | `~/.claude/skills/<name>/` |
| Codex | `codex` | `.codex/` | `.codex/skills/<name>/` (link) | `~/.codex/skills/<name>/` |
| Cursor | `cursor` | `.cursor/` | `.cursor/skills/<name>/` (link) | `~/.cursor/skills/<name>/` |
| Antigravity CLI | `antigravity` | never detected | `.agents/skills/<name>/` (shared) | `~/.gemini/antigravity-cli/skills/<name>/` |
| OpenCode | `opencode` | `.opencode/`, `opencode.json`, or `opencode.jsonc` | `.agents/skills/<name>/` (shared) | `~/.config/opencode/skills/<name>/` |
| OpenClaw | `openclaw` | never detected | `.agents/skills/<name>/` (shared) | `~/.openclaw/skills/<name>/` |
| Hermes Agent | `hermes` | `.hermes/` | `.agents/skills/<name>/` (shared) | `~/.hermes/skills/<name>/` |

Use an agent ID with `--agent` (e.g. `gskill add ./skill --agent opencode`). Any other ID fails with
exit **`9`**, and the error lists the supported IDs.

## Shared-location agents

Antigravity CLI, OpenCode, OpenClaw, and Hermes Agent read project skills from `.agents/skills/`,
the directory where GSKILL commits each skill. Installing a skill for one of them records the agent
in `skills-lock.json` with mode `shared` and writes no other file or link. `--copy` and `--symlink`
have no effect on these agents. They still apply to the other agents in the same command.

Because all four read that directory, **every project skill is visible to all four, whichever agents
you target.** Targeting decides what GSKILL records and what `--global` installs, not what these
agents can see.

OpenCode also reads `.claude/skills/`, so in a project that targets both `claude` and `opencode` it
can find the same skill twice. Both copies have identical content.

### Per-agent notes

- **Antigravity CLI**: global installs go to the CLI's own folder. The folder shared with the
  Antigravity IDE and desktop app (`~/.gemini/config/skills/`) is not written.
- **OpenCode**: OpenCode skips a skill whose frontmatter `name` differs from its directory, or is
  longer than 64 characters. GSKILL names the installed directory after the source folder, so when the
  two differ, `add`, `install`, and `gskill project check` print a warning naming the skill. The
  install itself still succeeds.
- **OpenClaw**: OpenClaw sees project skills only when the repository is its workspace. If you run
  OpenClaw from its default workspace, install with `--global`.
- **Hermes Agent**: Hermes loads a repository's skills only after you trust the project once with
  `hermes skills trust`. GSKILL prints this reminder after a project install that targets `hermes`,
  and `gskill doctor` lists it as a note.

## Detection

- When you don't pass `--agent`, GSKILL installs into the agents it **detects** in the project, using
  the markers in the table above.
- Antigravity CLI and OpenClaw have no project marker. Name them with `--agent`, in a skill's `agents`
  list in `skills.toml`, or in `defaults.agents`.
- `.agents/` never counts as a marker, because every GSKILL project has one.
- If you specify no agents and none are detected, GSKILL writes nothing and exits **`9`** (unsupported
  / undetected agent).
- `gskill doctor` reports which agents are detected.

## Scope

- **Project** (default): the content is committed at `.agents/skills/<name>/`. Claude Code, Codex, and
  Cursor each get a committed relative symlink into it; the shared-location agents read it directly.
- **Global** (`--global`): skills install as verified copies into each agent's user-global location
  (the last column above), outside the repo-owned model.

## Official GSKILL skills

GSKILL publishes two skills that let an agent install and drive GSKILL: `gskill-install` and
`gskill` (see [Use GSKILL from an AI agent](../how-to/use-gskill-from-an-agent.md)). Each agent
loads them from the folders below. "Checked" says whether loading and running them was tried on
the real agent, or follows from the adapter rules on this page.

| Agent | Loads from (project / global) | Load step | Checked |
| --- | --- | --- | --- |
| Claude Code | `.claude/skills/` / `~/.claude/skills/` | Start a new session. | Real agent: Claude Code 2.1.322, 2026-11-12 |
| Codex | `.codex/skills/` / `~/.codex/skills/` | Start a new session. | Real agent: codex-cli 0.167.0, 2026-11-12 |
| Cursor | `.cursor/skills/` / `~/.cursor/skills/` | Start a new session. | Adapter rules only |
| Antigravity CLI | `.agents/skills/` / `~/.gemini/antigravity-cli/skills/` | Start a new session. | Adapter rules only |
| OpenCode | `.agents/skills/` / `~/.config/opencode/skills/` | Start a new session. | Adapter rules only |
| OpenClaw | `.agents/skills/` / `~/.openclaw/skills/` | Project skills load only when the repository is the workspace. | Adapter rules only |
| Hermes Agent | `.agents/skills/` / `~/.hermes/skills/` | `hermes skills trust` once per project. | Adapter rules only |

The real-agent checks installed both skills in a project for `claude` and `codex` from a build of
this repository, then ran each agent non-interactively (`claude -p`, `codex exec`):

1. **Load**: asked for the skills it has whose names start with `gskill`. Both agents named
   `gskill` and `gskill-install`.
2. **Run**: asked it to use the `gskill` skill to list the project's skills. Both agents ran
   `gskill --json version` (Codex added `--no-interactive`), then
   `gskill --no-interactive --json list`, and reported both skills.

## See also

- [Target specific agents](../how-to/target-specific-agents.md)
- [Multi-agent installs](../explanation/multi-agent-installs.md)
