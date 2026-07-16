# Clean unused store objects

Old versions accumulate in the global store; garbage collection removes the
ones no project uses.

## Dry run (the default)

```sh
gskill store gc
```

lists deletable objects and the reclaimable size. Nothing is removed.

## Apply

```sh
gskill store gc --apply
```

An object is deleted only when **all** of these hold:

- no live project references it (lockfile, state file, or active link — each
  registered project is re-read at collection time, so a stale registry
  snapshot can never justify a deletion);
- it is not pinned;
- no gskill process holds its lock (in-flight objects are skipped and
  reported, and collection continues);
- its metadata is valid and it is safely owned;
- it is older than the grace period (default 30 days).

## Grace period

```sh
gskill config set store.gc_grace_period 60d   # persistent
gskill store gc --apply --older-than 90d      # per run
```

Removing a skill from a project never deletes store content — GC is the only
deletion path.
