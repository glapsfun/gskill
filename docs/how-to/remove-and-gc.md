# Remove a skill and reclaim space

Uninstall a skill from your agents, drop it from the lockfile, and garbage-collect its
content from the store.

## Before you start

- An installed skill you want to remove.

## Steps

```bash
gskill remove <name>
```

At an interactive terminal this asks for confirmation before removing
anything. In an unattended session (CI, a script, any run with no terminal
attached), `remove` now requires an explicit opt-in — pass `--force` (or the
existing `--yes`) so it knows deletion is intended:

```bash
gskill remove <name> --force
gskill remove <name-1> <name-2> --force   # multiple skills, one invocation
```

Without `--force`/`--yes` in a non-interactive session, `remove` aborts with
a non-zero exit and nothing is changed — no accidental deletes from a script
or CI job that forgot to opt in.

## Expected result

- The skill is uninstalled from every agent directory it was in.
- Its entry is removed from `skills-lock.json`.
- Store content no longer referenced by any skill is garbage-collected to reclaim space.
- `gskill remove` exits `0`.

## See also

- [Sync and repair](sync-and-repair.md)
- [Manage the cache](manage-the-cache.md)
