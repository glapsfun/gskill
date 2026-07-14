# Install a local skill

Add a skill from a directory on disk (a folder containing a `SKILL.md`). This is the fastest way to
try GSKILL and works completely offline.

## Before you start

- A project with an agent marker (e.g. a `.claude/` directory for Claude Code).
- A local skill folder containing a valid `SKILL.md` (see the
  [frontmatter schema](../reference/frontmatter-schema.md)).
- `gskill init` has been run in the project.

## Steps

```bash
gskill init                      # once per project, if not already done
gskill add ./path/to/skill       # the folder that contains SKILL.md
gskill list                      # confirm it installed
```

## Expected result

- The skill is installed into your detected agent's directory, e.g.
  `.claude/skills/<name>/SKILL.md`.
- `skills-lock.json` gains an entry recording both intent (source, constraint, agents) and resolved
  reality (content hash, targets).
- `gskill add` prints `Added <name> (<content-hash>) into N agent(s)` and exits `0`.
- Re-running `gskill install` reports **no changes** — installs are idempotent.

## See also

- [Add a skill from Git](add-a-git-skill.md)
- [Target specific agents](target-specific-agents.md)
- [The reproducibility model](../explanation/reproducibility-model.md)
