# Configuration reference

GSKILL resolves settings from layered sources. Inspect them with [`gskill config`](../how-to/configure-gskill.md).

## Precedence

From highest priority to lowest:

```text
command-line flags  >  GSKILL_* environment variables  >  project [config]  >  user config file  >  built-in defaults
```

A value set at a higher layer overrides the same value from any lower layer.

## Layers

| Layer | How to set | Example |
| --- | --- | --- |
| Flags | Pass on the command line | `gskill add ./skill --copy` |
| Environment | `GSKILL_*` variables | `GSKILL_LOG_LEVEL=debug gskill install --frozen-lockfile` |
| Project | The `[config]` table in the committed `skills.toml` | `[config]` `log_level = "debug"` |
| User config file | Edit `config.toml` (find it with `gskill config list`), or name one with `--config` | `log_level = "debug"` |
| Defaults | Built in | `log_level` defaults to `info` |

## The user config file

GSKILL reads one user-level config file per run. By default it is `config.toml` inside the
configuration directory — the exact path `gskill config list` prints on its first line, which
follows the platform convention and honors `GSKILL_CONFIG_DIR`. The file is optional: if it is
not there, the remaining layers apply and nothing is reported.

`gskill --config <path>` names a different file for that run. It **replaces** the discovered
file rather than layering on top of it, so a setting the named file leaves out falls through to
the project, environment, and defaults — never to the file at the default path. `gskill config
list` reports whichever of the two is in effect.

Because `--config` is a path you typed, it must exist: a missing path, or one that turns out to
be a directory, ends the run with the usage exit code (`2`) and a message naming it, rather than
silently falling back to the defaults. A file that exists but does not parse as TOML ends the run
with the generic error code (`1`), whether it was named or discovered.

Note that the file contributes to the *file* layer, not the flag layer: an explicit flag on the
command line still wins over anything the file says.

Run `gskill config list` to see both: it leads with the user config file path (labelled
`# user config:`, because the values below may have been overridden by the project `[config]`
table, a `GSKILL_*` variable, or a flag) and then prints the effective, fully-resolved
configuration. Its JSON form is `{"path": …, "values": {…}}` —
the settings are nested under `values` so that the config-key namespace stays free of `path`,
which is not a configuration key and is not accepted by `gskill config get`.

## Common settings

These mirror the manifest `[defaults]` block and the global flags:

| Setting | Values | Meaning |
| --- | --- | --- |
| `defaults.agents` | list of agent IDs | Target agents when an `add` specifies none. |
| `defaults.install_mode` | `symlink` \| `copy` \| `auto` | Default install mode. |
| `store.lock_timeout` | duration | Bounds the project mutate-lock wait (name kept for config compatibility). |
| `offline` | bool (flag `--offline`) | Operate without network. |
| `no_cache` | bool (flag `--no-cache`) | Bypass the clone cache. |

> **Known limitation.** `offline` and `no_cache` currently take effect **only** as command-line
> flags. Setting them in a config file or via `GSKILL_OFFLINE` / `GSKILL_NO_CACHE` changes what
> `gskill config list` reports but does not change what any command does. Use `--offline` and
> `--no-cache` until this is fixed.

## Removed keys

The pre-022 global-store keys — `store.scope`, `store.verify_on_use`, `store.gc_grace_period`,
`projects.registry`, `privacy.project_registry` (and their env forms `GSKILL_STORE_SCOPE`,
`GSKILL_STORE_VERIFY`, `GSKILL_PROJECT_REGISTRY`) — no longer exist. If present in a config file
they are **ignored, never an error**.

For the complete flag list, see the [command reference](commands.md). For where files live on your
machine, see [the clone cache](../explanation/store-and-cache.md).

## See also

- [Configure GSKILL](../how-to/configure-gskill.md)
- [Command reference](commands.md)
