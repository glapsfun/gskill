# Manage the cache

Inspect and maintain GSKILL's clone cache — the commit-keyed git clones that make offline restores
and fast re-installs possible.

## Before you start

- GSKILL installed. A warm cache builds up as you `add`/`install`.

## Subcommands

```bash
gskill cache stats       # cache directory, entry count, and size
gskill cache list        # list commit-keyed entries
gskill cache clean       # remove all cached material
```

## Expected result

- `stats`, `list`, and `path` are read-only and exit `0`.
- `clean` empties the cache; subsequent installs will need to re-fetch (so don't run it right before an
  offline restore).

> The cache is shared by **every project on the machine**, so `cache clean` affects all of them. It
> is still safe: nothing in any repository references the cache, and missing content is simply
> re-fetched on demand.

## Where the cache lives

The cache lives in your home directory at `$HOME/.gskill/cache/<commit>/` (relocatable only via
`GSKILL_HOME`), one entry per resolved commit. Run `gskill cache stats` to see the exact location on
your machine; `gskill cache stats --json` exposes it as the `path` field.

## See also

- [Work offline](work-offline.md)
- [The clone cache](../explanation/store-and-cache.md)
