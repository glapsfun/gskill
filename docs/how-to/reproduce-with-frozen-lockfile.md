# Reproduce exactly with `--frozen-lockfile`

Restore a byte-identical skill environment from a committed `skills-lock.json`, with a guarantee that the
lockfile is never modified. This is the command CI and teammates should use.

## Before you start

- A committed `skills-lock.json` (see the [tutorial](../tutorials/getting-started.md)).
- The project checked out (the lockfile present at the project root).

> A fresh `git clone` already contains the committed skill content and links — it works with zero
> gskill commands. Run the frozen restore to *prove* the checkout matches the lock, or to
> re-materialise content that has been deleted.

## Steps

```bash
# On a fresh checkout (or after deleting installed content):
gskill install --frozen-lockfile
```

## Expected result

- GSKILL re-materialises exactly what the lockfile records and exits `0` — committed content that
  matches the lock is left as-is (zero network), missing content is restored from the clone cache
  by its recorded commit, and a cold cache fetches exactly once from the recorded source.
- The lockfile is **not** rewritten — a frozen restore is read-only with respect to `skills-lock.json`.
- If an entry lacks its gskill metadata (or a resolved artifact no longer matches its recorded
  checksum), the command **fails closed**: it exits **`4`** (lockfile mismatch) and modifies **zero**
  agent directories.

### Verifying the fail-closed behaviour

```bash
# Hand-edit a computedHash in skills-lock.json, then:
gskill install --frozen-lockfile
echo "exit: $?"        # prints: exit: 4
```

## See also

- [Work offline](work-offline.md) — frozen restore from a warm cache, no network.
- [Gate CI on drift](gate-ci-on-drift.md)
- [Exit codes](../reference/exit-codes.md)
