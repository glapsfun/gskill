# Troubleshooting the global store

## "corrupted global store object" / exit 6

An object's content no longer matches its hash. The object was quarantined
under `~/.gskill/quarantine/` and never activated.

```sh
gskill store verify                  # find every corrupted object and its users
gskill store repair sha256:<hash>    # restore from the recorded exact commit
```

If repair reports that the exact source cannot be reproduced, re-install the
skill in any project that locks it — admission re-creates the object.

## "not available in the global store" with `--offline`

The lockfile references an object this machine has never fetched. Run once
without `--offline` (or on a machine with the object, then sync the home).

## "waiting for lock …" or "acquire lock … within 60s"

Another gskill process holds the object or project lock. If no process is
actually running, a crashed run may have left the lock file; locks are
advisory flocks, so simply retry — the kernel released the lock with the
process. Raise `store.lock_timeout` for slow shared filesystems.

## "global store is writable by other users"

```sh
chmod -R go-w ~/.gskill
```

Objects owned by another user are refused outright; remove them or fix
ownership.

## Broken links after moving the home or project

Links are generated state. Run `gskill repair` in the project: it recreates
the active link, agent links, and copy-mode content from the store, without
network access when the object exists.

## Legacy project-local store

`gskill doctor` suggests `gskill migrate global-store` when it detects
`<project>/.gskill/store`. See
[migrate to the global store](../how-to/migrate-to-global-store.md).
