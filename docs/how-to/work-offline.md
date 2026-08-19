# Work offline

Restore skills without network access. This is useful on planes, in air-gapped CI, or whenever you
want to guarantee no network calls.

> A fresh `git clone` already contains the skills — committed content needs **zero** gskill
> commands and zero network. You only need the steps below to re-materialise deleted or missing
> content.

## Before you start

- A committed `skills-lock.json`.
- Committed skill content, **or** a warm clone cache (the source was fetched at least once on this
  machine, e.g. by a previous `gskill add` or `gskill install`).

## Steps

```bash
gskill --offline install --frozen-lockfile
```

## Expected result

- Committed content that matches the lock is up to date without any work; anything missing is
  restored from the clone cache by its recorded commit. GSKILL exits `0` — no network is touched.
- If something required is missing from both the repo and the cache, GSKILL fails closed rather
  than reaching out: expect a non-zero exit (source unavailable, `5`) with a clear diagnostic.

## Tips

- `--offline` is a global flag, so it can precede any command.
- `--no-cache` does the opposite — it bypasses the cache. Don't combine it with `--offline`.
- Inspect the cache with [`gskill cache`](manage-the-cache.md).

## See also

- [Reproduce with --frozen-lockfile](reproduce-with-frozen-lockfile.md)
- [The clone cache](../explanation/store-and-cache.md)
