# Supported agents

GSKILL installs skills into AI coding agents. v1 ships four first-class adapters. Each agent is
detected by a **marker directory** in your project and has its own **skills directory** where GSKILL
installs content.

## Adapters

| Agent | Agent ID | Marker | Project install path |
| --- | --- | --- | --- |
| Claude Code | `claude-code` | `.claude/` | `.claude/skills/<name>/` |
| Codex | `codex` | `.codex/` | `.codex/skills/<name>/` |
| Cursor | `cursor` | `.cursor/` | `.cursor/skills/<name>/` |
| Gemini CLI | `gemini-cli` | `.gemini/` | `.gemini/skills/<name>/` |

Use an agent ID with `--agent` (e.g. `gskill add ./skill --agent codex`). All four support symlink
installs.

## Detection

- When you don't pass `--agent`, GSKILL installs into the agents it **detects** in the project — by the
  presence of their marker directory (`.claude/`, `.codex/`, `.cursor/`, `.gemini/`).
- If you specify no agents and none are detected, GSKILL writes nothing and exits **`9`** (unsupported
  / undetected agent).
- `gskill doctor` reports which agents are detected.

## Scope

- **Project** (default): skills install under the project's agent directories (paths above).
- **Global** (`--global`): skills install into your user-global location for that agent. Global paths
  follow platform conventions (XDG on Linux, the platform equivalents on macOS and Windows).

## See also

- [Target specific agents](../how-to/target-specific-agents.md)
- [Multi-agent installs](../explanation/multi-agent-installs.md)
