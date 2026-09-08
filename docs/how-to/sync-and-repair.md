# Sync and repair

Reconcile what's on disk with the lockfile, and fix installs that have gone missing or broken.

## Before you start

- A committed `skills-lock.json`.

## Make disk match the lock

```bash
gskill project sync              # install anything missing so disk matches the lock
gskill project sync --prune      # also remove gskill-managed installs not in the lock
```

**Expected:** `sync` makes the installed state match the lockfile. Plain `sync` is additive; add
`--prune` to delete orphaned installs that the lock no longer references.

> `install` is additive and never deletes. `sync --prune` is the destructive reconciler — use it when
> you want disk to be an exact mirror of the lock.

`--prune` only removes **gskill-managed** installs: entries in an agent's skill directory that are
symlinks into `.agents/skills/`. Skills you installed by hand, or that another tool placed in the
same shared directory (e.g. `.claude/skills/`), are left untouched. Copy-mode installs carry no such
marker — remove those explicitly with `gskill remove <name>`.

## Repair broken installs

```bash
gskill project repair
```

**Expected:** GSKILL re-materialises broken or missing installs — from the committed content, the
clone cache, or (cold cache) a single fetch from the recorded source — and cleans up leftover
staging, **without** changing the lockfile.

## Hand-edited content

If committed skill content was edited by hand, plain `sync` (like `add` and `install`) fails with
"committed content for skill X ... no longer matches skills-lock.json" and the hint to run
`gskill project repair` (or `gskill install --force`) to restore lock-true content, or re-add the skill to
adopt the edits. Drift is never auto-repaired.

## Expected result

- `sync` / `sync --prune` / `repair` each exit `0` on success and leave the lockfile unchanged.

## See also

- [Reproduce with --frozen-lockfile](reproduce-with-frozen-lockfile.md)
- [Verify integrity](verify-integrity.md)
