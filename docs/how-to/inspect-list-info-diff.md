# Inspect with list, info, and diff

See what's installed, drill into one skill, and compare the lockfile with disk.

## Before you start

- A project with at least one skill declared or installed.

## List installed skills

```bash
gskill list              # table of skills + status
gskill list --json       # machine-readable
```

## Show one skill in detail

```bash
gskill info <name>       # details, frontmatter, and declared requirements
```

**Expected:** identity, resolved version/commit, target agents, and the skill's `requires` block (which
GSKILL records and warns about but does not resolve transitively).

## Compare intent, reality, and disk

```bash
gskill project diff              # all skills
gskill project diff <name>       # one skill
```

**Expected:** the differences between `gskill.toml` (intent), `skills-lock.json` (reality), and what's
actually installed — so you can see exactly what a `sync`, `update`, or `install` would change.

## See also

- [Sync and repair](sync-and-repair.md)
- [`skills-lock.json` schema](../reference/lockfile-schema.md)
