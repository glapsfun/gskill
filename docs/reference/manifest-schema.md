# `gskill.toml` manifest schema (v1)

The manifest is the **human-editable record of intent**. You edit it; GSKILL reads it. Anything GSKILL
computes (URLs, resolved versions, hashes, targets) belongs in the [lockfile](lockfile-schema.md), not
here. Format: TOML. `schema_version = 1`.

## Shape

```toml
schema_version = 1

[defaults]
agents = ["claude-code", "codex"]   # default target agents when an add specifies none
install_mode = "symlink"            # symlink | copy | auto
scope = "project"                   # project | global

[skills.<name>]                     # <name>: lowercase kebab [a-z0-9-]; must match frontmatter name
source = "github.com/owner/repo"    # required: git shorthand | git URL | ./local path | url
path = "skills/<name>"              # optional: in-repo subpath (explicit path wins over discovery)
version = "^2.0.0"                  # optional: semver constraint
ref = "main"                        # optional: branch/tag (mutable — flagged in the lock)
commit = "<sha>"                    # optional: explicit commit pin
agents = ["claude-code"]            # optional: overrides defaults.agents
install_mode = "symlink"            # optional: overrides defaults.install_mode
```

## Field rules

| Field | Required | Type / values | Notes |
| --- | --- | --- | --- |
| `schema_version` | yes | int | Refused if greater than the tool understands. |
| `defaults.agents` | no | list of agent IDs | Known IDs only (see [agents](agents.md)). |
| `defaults.install_mode` | no | `symlink` \| `copy` \| `auto` | Default install mode. |
| `defaults.scope` | no | `project` \| `global` | Defaults to `project`. |
| `[skills.<name>]` key | yes (≥0 entries) | lowercase kebab `[a-z0-9-]` | Must equal the skill's frontmatter `name`. |
| `skills.<name>.source` | yes | string | Parseable to a Git, local, or URL source. |
| `skills.<name>.path` | no | string | In-repo subpath to the skill. |
| `skills.<name>.version` | no | semver constraint | At most one of version/ref/commit drives resolution. |
| `skills.<name>.ref` | no | string | Branch or tag (mutable; resolved to a commit in the lock). |
| `skills.<name>.commit` | no | string | Explicit commit pin. |
| `skills.<name>.agents` | no | list of agent IDs | Overrides `defaults.agents`. |
| `skills.<name>.install_mode` | no | `symlink` \| `copy` \| `auto` | Overrides `defaults.install_mode`. |

## Forward compatibility

- **Unknown skill-scoped keys** → warning, not an error (forward-compatible).
- **Unknown top-level sections** → rejected (protects manifest integrity).
- A `schema_version` newer than the tool understands is refused with an upgrade message.

## See also

- [`gskill.lock` schema](lockfile-schema.md) — the resolved, verified counterpart.
- [`SKILL.md` frontmatter schema](frontmatter-schema.md)
- [The reproducibility model](../explanation/reproducibility-model.md)
