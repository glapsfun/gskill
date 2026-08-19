# Install a local skill

Add a skill from a directory on disk (a folder containing a `SKILL.md`). This is the fastest way to
try GSKILL and works completely offline.

## Before you start

- A project with an agent marker (e.g. a `.claude/` directory for Claude Code).
- A local skill folder containing a valid `SKILL.md` (see the
  [frontmatter schema](../reference/frontmatter-schema.md)).

## Steps

```bash
gskill add ./path/to/skill       # the folder that contains SKILL.md (auto-initializes the project)
gskill list                      # confirm it installed
```

## Expected result

- The skill content lands at `.agents/skills/<name>/`, and your detected agent gets a relative
  symlink to it, e.g. `.claude/skills/<name>` — so `.claude/skills/<name>/SKILL.md` resolves.
- `skills-lock.json` gains an entry recording both intent (source, constraint, agents) and resolved
  reality (content hash, targets).
- `gskill add` prints `Added <name> (<content-hash>) into N agent(s)` and exits `0`.
- `git status` lists `.agents/skills/<name>/` and the agent link as addable — **commit them** along
  with `skills-lock.json`; a fresh clone then works with zero gskill commands.
- Re-running `gskill install` reports **no changes** — installs are idempotent.

## See also

- [Add a skill from Git](add-a-git-skill.md)
- [Target specific agents](target-specific-agents.md)
- [The reproducibility model](../explanation/reproducibility-model.md)
