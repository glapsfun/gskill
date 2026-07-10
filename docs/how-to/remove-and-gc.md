# Remove a skill and reclaim space

Uninstall a skill from your agents, drop it from the lockfile, and garbage-collect its
content from the store.

## Before you start

- An installed skill you want to remove.

## Steps

```bash
gskill remove <name>
```

## Expected result

- The skill is uninstalled from every agent directory it was in.
- Its entry is removed from both `gskill.toml` and `skills-lock.json`.
- Store content no longer referenced by any skill is garbage-collected to reclaim space.
- `gskill remove` exits `0`.

## See also

- [Sync and repair](sync-and-repair.md)
- [Manage the cache](manage-the-cache.md)
