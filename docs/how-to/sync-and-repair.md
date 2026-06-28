# Sync and repair

Reconcile what's on disk with the lockfile, and fix installs that have gone missing or broken.

## Before you start

- A committed `gskill.lock`.

## Make disk match the lock

```bash
gskill sync              # install anything missing so disk matches the lock
gskill sync --prune      # also remove gskill-managed installs not in the lock
```

**Expected:** `sync` makes the installed state match the lockfile. Plain `sync` is additive; add
`--prune` to delete orphaned installs that the lock no longer references.

> `install` is additive and never deletes. `sync --prune` is the destructive reconciler — use it when
> you want disk to be an exact mirror of the lock.

`--prune` only removes **gskill-managed** installs: entries in an agent's skill directory that are
symlinks into the gskill store. Skills you installed by hand, or that another tool placed in the same
shared directory (e.g. `.claude/skills/`), are left untouched. Copy-mode installs carry no such
marker — remove those explicitly with `gskill remove <name>`.

## Repair broken installs

```bash
gskill repair
```

**Expected:** GSKILL re-materialises broken or modified installs from the store/cache and cleans up
leftover staging, **without** changing the lockfile.

## Expected result

- `sync` / `sync --prune` / `repair` each exit `0` on success and leave the lockfile unchanged.

## See also

- [Reproduce with --frozen-lockfile](reproduce-with-frozen-lockfile.md)
- [Verify integrity](verify-integrity.md)
