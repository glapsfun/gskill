# Migrate a project to the global store

Projects created before the global store keep their content in
`<project>/.gskill/store` and continue to work unchanged. Migration is
explicit, dry-runnable, and rollback-safe.

## Preview

```sh
gskill migrate global-store --dry-run
```

reports the objects found, how many already exist globally, how many would
be copied, and the disk savings — changing nothing.

## Migrate

```sh
gskill migrate global-store
```

The command verifies every local object, dedupes or copies it into the
global store (verifying again after admission), re-points the project-active
links, records `.gskill/state.json`, registers the project, and only after
complete success removes the legacy local store.

## Safety

- A corrupt local object is skipped and reported; the local store is then
  preserved and the project stays fully usable.
- Any failure before the final removal leaves the legacy layout intact —
  rollback is by construction, not by undo.
- Re-running on a migrated project is a no-op.
