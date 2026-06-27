# Add a skill from Git

Add a skill hosted in a Git repository, pinning it to a version constraint, branch/tag, or exact
commit. GSKILL resolves a floating reference to an immutable commit before writing the lockfile, so
the install is reproducible.

> This guide is verified manually against a real Git remote; the example uses a placeholder source.

## Before you start

- A project with an agent marker and `gskill init` run.
- Network access to the Git host (or a warm cache — see [Work offline](work-offline.md)).
- A system `git` binary on `PATH` (`gskill doctor` checks this).

## Steps

```bash
# Track a semver range (recommended):
gskill add github.com/<org>/<repo>/<skill> --version '^1.0.0'

# Or pin to a branch/tag:
gskill add github.com/<org>/<repo>/<skill> --ref main

# Or pin to an exact commit:
gskill add github.com/<org>/<repo>/<skill> --commit <sha>
```

## Expected result

- GSKILL resolves the source, fetches it, installs the skill, and records both intent (`gskill.toml`)
  and resolved reality (`gskill.lock`) — including the exact commit and content hash.
- A mutable reference (like a branch) is resolved to an immutable commit in the lock and flagged as
  mutable.
- `gskill add` exits `0`. Adding a skill whose key already exists errors and points you to `update` or
  `--force`.

## See also

- [Install a local skill](install-a-local-skill.md)
- [Update and re-lock](update-and-lock.md)
- [`gskill.lock` schema](../reference/lockfile-schema.md)
