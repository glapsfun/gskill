# Configure GSKILL

Inspect layered configuration — defaults that apply across commands — and set values by editing the
config file.

## Before you start

- GSKILL installed.

## Subcommands

```bash
gskill config list       # print the effective configuration
gskill config get <key>  # print one value
gskill config path       # print the config file path
```

To change a value, edit the `config.toml` that `gskill config path` prints (or set the matching
`GSKILL_*` environment variable).

## Configuration precedence

GSKILL resolves each setting from the highest-priority source that provides it:

```text
command-line flags  >  GSKILL_* environment variables  >  config file  >  built-in defaults
```

So a flag always wins over an environment variable, which wins over the config file, which wins over
the defaults. See the [configuration reference](../reference/configuration.md) for the keys and their
environment-variable forms.

## Expected result

- `list`, `get`, and `path` are read-only and exit `0`.

## See also

- [Configuration reference](../reference/configuration.md)
- [Command reference](../reference/commands.md)
