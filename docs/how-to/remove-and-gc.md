# Remove a skill and reclaim space

Uninstall a skill from your agents, drop it from the lockfile, and delete its committed content
from the repository.

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

- The skill's agent links are removed from every agent directory it was in.
- Its committed copy at `.agents/skills/<name>/` is deleted.
- Its entry is removed from `skills-lock.json`.
- `gskill remove` exits `0`. Commit the deletions to share them.

> There is no store garbage collection — content lives in the repository, so removing it *is*
> reclaiming the space. The clone cache in your home directory is untouched; clear it separately
> with [`gskill cache clean`](manage-the-cache.md) if you want the disk back there too.

## See also

- [Sync and repair](sync-and-repair.md)
- [Manage the cache](manage-the-cache.md)
