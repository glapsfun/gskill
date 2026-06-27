# The store and the cache

GSKILL keeps two content-addressed areas under the hood: the **store** (installed content) and the
**cache** (fetched material). Understanding the difference explains how offline restores, symlink
installs, and `gskill cache` work.

## The store: installed content

The store holds the actual skill content that installs point at, addressed by content hash. When you
install with the default `--symlink` mode, an agent's `skills/<name>` entry links into the store rather
than holding its own copy. This means:

- Restores are fast and disk-efficient — shared content is stored once.
- Verification still catches tampering, because edits made through a symlink land in the store content
  that the lockfile's checksum covers.

When a skill is removed and nothing else references its content, that content is garbage-collected from
the store (see [Remove a skill](../how-to/remove-and-gc.md)).

## The cache: fetched material

The cache holds the raw material GSKILL fetched from a source (e.g. a Git repository), addressed by
content hash. It is what makes **offline restores** possible: if the cache is warm, GSKILL can restore
from the lock without any network. Manage it with [`gskill cache`](../how-to/manage-the-cache.md):
`stats`, `list`, `path`, and `clean`.

- `--offline` tells GSKILL to use only the cache and never reach the network.
- `--no-cache` does the opposite: bypass the cache.

## Where they live (cross-platform)

Both areas live under the project's `.gskill/` state directory for project-scoped work, or in your
user-level locations for global installs. Paths follow platform conventions:

| OS | User-level location |
| --- | --- |
| Linux | XDG base directories (e.g. `$XDG_CACHE_HOME`, `$XDG_CONFIG_HOME`) |
| macOS | The platform equivalents under your home directory |
| Windows | `%LOCALAPPDATA%` / `%APPDATA%` |

A `GSKILL_`-prefixed environment can override locations. Use `gskill cache path` to print the exact
directory on your machine.

## See also

- [Work offline](../how-to/work-offline.md)
- [Copy vs symlink](../how-to/copy-vs-symlink.md)
- [Manage the cache](../how-to/manage-the-cache.md)
