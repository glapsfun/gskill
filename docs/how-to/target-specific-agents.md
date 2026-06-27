# Target specific agents

Install a skill into one or more specific AI agents, and choose whether it lands in the project or in
your user-global location.

## Before you start

- A project with at least one supported agent marker, or the intent to target an agent explicitly.
- Supported agent IDs: `claude-code`, `codex`, `cursor`, `gemini-cli` (see
  [Supported agents](../reference/agents.md)).

## Choose agents

```bash
# Install into a single agent:
gskill add ./skill --agent codex

# Install into several (repeat --agent):
gskill add ./skill --agent claude-code --agent cursor
```

If you pass no `--agent`, GSKILL installs into the agents it detects in the project (via their marker
directories, e.g. `.claude/`, `.codex/`, `.cursor/`, `.gemini/`).

## Choose scope

```bash
gskill add ./skill --project     # into this project (default)
gskill add ./skill --global      # into your user-global location
```

## Expected result

- The skill is installed into each chosen agent's skills directory (e.g. `.claude/skills/<name>/`,
  `.codex/skills/<name>/`).
- The lockfile records the targeted agents and their install paths.
- If you specify no agents **and** none are detected, GSKILL writes nothing and exits **`9`**
  (unsupported / undetected agent). *(Verified manually.)*

## See also

- [Supported agents](../reference/agents.md)
- [Multi-agent installs](../explanation/multi-agent-installs.md)
