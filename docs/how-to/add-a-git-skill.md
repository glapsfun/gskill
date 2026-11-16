# Add a skill from Git

Add a skill hosted in a Git repository, pinning it to a version constraint, branch/tag, or exact
commit. GSKILL resolves a floating reference to an immutable commit before writing the lockfile, so
the install is reproducible.

> This guide is verified manually against a real Git remote; the example uses a placeholder source.

## Before you start

- A project with an agent marker (`add` initializes the rest automatically).
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

# GitLab and Bitbucket work the same way, over HTTPS or SSH:
gskill add https://gitlab.com/<group>/<subgroup>/<project>.git --skill <skill>
gskill add git@bitbucket.org:<workspace>/<repo>.git --skill <skill>

# Name a folder inside the repository with //:
gskill add gitlab.com/<group>/<subgroup>/<project>//<skill-folder>
```

A GitLab path deeper than two segments needs `.git` after the project or `//` before the folder;
see the [sources reference](../reference/sources.md). For private repositories, see
[Use private and non-GitHub repositories](use-private-repositories.md).

## Expected result

- GSKILL resolves the source, fetches it once into the clone cache, writes the skill into
  `.agents/skills/<name>/` with a relative agent link, and records both intent (the `gskill` block)
  and resolved reality (`skills-lock.json`) — including the exact commit and content hash.
- A mutable reference (like a branch) is resolved to an immutable commit in the lock and flagged as
  mutable.
- `add` exits `0`. Adding a skill whose key already exists errors and points you to `update` or
  `--force`.
- `git status` now lists `.agents/skills/<name>/` and the agent link (e.g. `.claude/skills/<name>`)
  as addable alongside `skills-lock.json` — **commit them**; the content belongs in the repository.

## See also

- [Sources reference](../reference/sources.md)
- [Use private and non-GitHub repositories](use-private-repositories.md)
- [Install a local skill](install-a-local-skill.md)
- [Update and re-lock](update-and-lock.md)
- [`skills-lock.json` schema](../reference/lockfile-schema.md)
