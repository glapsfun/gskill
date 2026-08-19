# The clone cache

GSKILL keeps one machine-level cache under the hood: commit-keyed git clones of the sources your
skills come from. Understanding it explains how offline restores, fast re-installs, and
`gskill cache` work. (Installed content itself lives **in the repository** — see
[repo-owned storage](repo-owned-storage.md).)

## What the cache holds

The cache stores the raw material GSKILL fetched from a source (e.g. a Git repository), one entry
per resolved commit at `cache/<40-hex-commit>/`. It is what makes **offline restores** possible: if
the cache is warm, GSKILL can restore from the lock without any network. Manage it with
[`gskill cache`](../how-to/manage-the-cache.md): `stats`, `list`, `path`, and `clean`.

- `--offline` tells GSKILL to use only the cache and never reach the network.
- `--no-cache` does the opposite: bypass the cache.

## A pure performance cache

Nothing in the repository ever references the cache: reproduction comes from `skills-lock.json`
plus the committed content. Deleting the cache breaks nothing — the next install that needs missing
material simply re-fetches it. The cache is shared by every project on the machine, so a skill
fetched for one project restores instantly in another.

## Where it lives

The cache lives under `$HOME/.gskill` (relocatable only via the `GSKILL_HOME` environment
variable), alongside the only other things kept there: `locks/`, `tmp/`, and `config.toml`.
GSKILL supports macOS and Linux. Use `gskill cache path` to print the exact directory on your
machine.

## See also

- [Repo-owned storage](repo-owned-storage.md)
- [Work offline](../how-to/work-offline.md)
- [Manage the cache](../how-to/manage-the-cache.md)
