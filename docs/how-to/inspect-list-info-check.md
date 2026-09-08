# Inspect with list, info, and check

See what's installed, drill into one skill, and compare the lockfile with disk.

## Before you start

- A project with at least one skill declared or installed.

## List installed skills

```bash
gskill list              # table of skills + status
gskill list --json       # machine-readable
```

**Expected:** one row per skill with its status, resolved version, source, and per-agent health.

## Show one skill in detail

```bash
gskill info <name>       # details, frontmatter, and declared requirements
```

**Expected:** identity, resolved version/commit, target agents, and the skill's `requires` block (which
GSKILL records and warns about but does not resolve transitively).

## Compare intent, reality, and disk

```bash
gskill project check                    # drift report for every declared skill
gskill project check --json             # machine-readable
gskill project check --fail-on-drift    # exit 7 when anything has drifted
```

**Expected:** the differences between `skills-lock.json` (intent + resolved reality) and what's
actually installed — so you can see exactly what a `project sync`, `update`, or `install` would
change. On top of the name and status, `check` names the override input that changed and reports
the faults behind each drifted skill.

> The former `gskill project diff` was retired: its report was a strict subset of `project check`.
> Use `gskill list` for the per-skill table and `gskill project check --json` for scripting.

## See also

- [Sync and repair](sync-and-repair.md)
- [Verify integrity](verify-integrity.md)
- [`skills-lock.json` schema](../reference/lockfile-schema.md)
