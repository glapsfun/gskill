# Configure GSKILL

Inspect and set layered configuration — defaults that apply across commands.

## Before you start

- GSKILL installed.

## Subcommands

```bash
gskill config list       # print the effective configuration
gskill config get <key>  # print one value
gskill config set <key> <value>   # set a value
gskill config path       # print the config file path
```

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
- `set` writes to the config file and exits `0`.

## See also

- [Configuration reference](../reference/configuration.md)
- [Command reference](../reference/commands.md)
