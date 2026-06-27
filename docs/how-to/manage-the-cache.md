# Manage the cache

Inspect and maintain GSKILL's content cache — the fetched material that makes offline restores and fast
re-installs possible.

## Before you start

- GSKILL installed. A warm cache builds up as you `add`/`install`.

## Subcommands

```bash
gskill cache stats       # cache size and entry count
gskill cache list        # list cached entries
gskill cache path        # print the cache directory
gskill cache clean       # remove all cached material
```

## Expected result

- `stats`, `list`, and `path` are read-only and exit `0`.
- `clean` empties the cache; subsequent installs will need to re-fetch (so don't run it right before an
  offline restore).

## Where the cache lives

The cache is content-addressed and lives under the project's `.gskill/` state directory, or in your
user cache location for global installs. Paths follow platform conventions (XDG on Linux, the platform
equivalents on macOS and Windows). Use `gskill cache path` to see the exact location on your machine.

## See also

- [Work offline](work-offline.md)
- [The store and the cache](../explanation/store-and-cache.md)
