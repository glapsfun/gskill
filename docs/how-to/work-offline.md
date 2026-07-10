# Work offline

Restore skills without network access, using GSKILL's content cache. This is useful on planes, in
air-gapped CI, or whenever you want to guarantee no network calls.

## Before you start

- A committed `skills-lock.json`.
- A **warm cache**: the content was fetched at least once on this machine (e.g. a previous
  `gskill add` or `gskill install`).

## Steps

```bash
gskill --offline install --frozen-lockfile
```

## Expected result

- If the cache contains everything the lockfile needs, GSKILL restores it and exits `0` — no network
  is touched.
- If something required is missing from the cache, GSKILL fails closed rather than reaching out:
  expect a non-zero exit (source unavailable, `5`) with a clear diagnostic.

## Tips

- `--offline` is a global flag, so it can precede any command.
- `--no-cache` does the opposite — it bypasses the cache. Don't combine it with `--offline`.
- Inspect the cache with [`gskill cache`](manage-the-cache.md).

## See also

- [Reproduce with --frozen-lockfile](reproduce-with-frozen-lockfile.md)
- [The store and the cache](../explanation/store-and-cache.md)
