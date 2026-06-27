# Configuration reference

GSKILL resolves settings from layered sources. Manage them with [`gskill config`](../how-to/configure-gskill.md).

## Precedence

From highest priority to lowest:

```text
command-line flags  >  GSKILL_* environment variables  >  config file  >  built-in defaults
```

A value set at a higher layer overrides the same value from any lower layer.

## Layers

| Layer | How to set | Example |
| --- | --- | --- |
| Flags | Pass on the command line | `gskill add ./skill --copy` |
| Environment | `GSKILL_*` variables | `GSKILL_OFFLINE=1 gskill install --frozen-lockfile` |
| Config file | `gskill config set <key> <value>` | `gskill config set defaults.install_mode copy` |
| Defaults | Built in | `install_mode` defaults to `symlink` |

Run `gskill config path` to find the active config file, and `gskill config list` to print the
effective, fully-resolved configuration.

## Common settings

These mirror the manifest `[defaults]` block and the global flags:

| Setting | Values | Meaning |
| --- | --- | --- |
| `defaults.agents` | list of agent IDs | Target agents when an `add` specifies none. |
| `defaults.install_mode` | `symlink` \| `copy` \| `auto` | Default install mode. |
| `defaults.scope` | `project` \| `global` | Default install scope. |
| offline | bool (flag `--offline` / `GSKILL_OFFLINE`) | Operate without network. |
| cache | bool (flag `--no-cache` / `GSKILL_NO_CACHE`) | Bypass the content cache. |

For the complete flag list, see the [command reference](commands.md). For where files live across
operating systems, see [the store and the cache](../explanation/store-and-cache.md).

## See also

- [Configure GSKILL](../how-to/configure-gskill.md)
- [Command reference](commands.md)
