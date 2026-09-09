# Configure GSKILL

Inspect layered configuration — defaults that apply across commands — and set values by editing the
config file.

## Before you start

- GSKILL installed.

## Subcommands

```bash
gskill config list       # print the config file path and the effective configuration
gskill config get <key>  # print one value
```

To change a value, edit the `config.toml` whose path `gskill config list` labels on its first line
(or set the matching `GSKILL_*` environment variable). In JSON the payload is
`{"path": …, "values": {…}}`, so `gskill config list --json | jq -r .path` gives the file and
`jq -r .values.log_level` gives one setting.

## Using a different config file

```bash
gskill --config ./ci.toml install    # use ./ci.toml instead of the user config file
```

`--config` replaces the user config file for that run rather than layering on top of it, and the
path must exist — a typo exits `2` and names the path instead of quietly falling back to the
defaults. `gskill config list` then reports the named file as the one in effect.

## Configuration precedence

GSKILL resolves each setting from the highest-priority source that provides it:

```text
command-line flags  >  GSKILL_* environment variables  >  project [config]  >  user config file  >  built-in defaults
```

So a flag always wins over an environment variable, which wins over the `[config]` table in the
project's `skills.toml`, which wins over the user config file, which wins over the defaults. See the
[configuration reference](../reference/configuration.md) for the keys and their environment-variable
forms.

## Expected result

- `list` and `get` are read-only and exit `0`.

## See also

- [Configuration reference](../reference/configuration.md)
- [Command reference](../reference/commands.md)
