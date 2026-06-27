# Reproduce exactly with `--frozen-lockfile`

Restore a byte-identical skill environment from a committed `gskill.lock`, with a guarantee that the
lockfile is never modified. This is the command CI and teammates should use.

## Before you start

- A committed `gskill.toml` and `gskill.lock` (see the [tutorial](../tutorials/getting-started.md)).
- The project checked out (the lockfile present at the project root).

## Steps

```bash
# On a fresh checkout (or after deleting installed state):
gskill install --frozen-lockfile
```

## Expected result

- GSKILL re-materialises exactly what the lockfile records and exits `0`.
- The lockfile is **not** rewritten — a frozen restore is read-only with respect to `gskill.lock`.
- If the manifest and lockfile disagree (or a resolved artifact no longer matches its recorded
  checksum), the command **fails closed**: it exits **`4`** (lockfile mismatch) and modifies **zero**
  agent directories.

### Verifying the fail-closed behaviour

```bash
# Edit gskill.toml so it disagrees with the lock, then:
gskill install --frozen-lockfile
echo "exit: $?"        # prints: exit: 4
```

## See also

- [Work offline](work-offline.md) — frozen restore from a warm cache, no network.
- [Gate CI on drift](gate-ci-on-drift.md)
- [Exit codes](../reference/exit-codes.md)
