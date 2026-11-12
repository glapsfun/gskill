# Workflows

One section per request. Each gives the command, what it changes, and how to confirm it worked.

Conventions for every command:

- Pass `--no-interactive` to every command. Add `--json` when you will read the result.
- Run in the project root, or pass `-C <dir>` before the command for another project.
- `skills.toml` is the declaration (what the project asks for). `skills-lock.json` is the record
  of what was resolved. "Content" is `.agents/skills/<name>/` plus each agent's link to it.
- Errors print on stderr as `error: <message>`, often followed by a `→ <hint>` line. Some
  failures, such as `project verify` exit 6, still print JSON on stdout. Read the exit code first
  ([results](results.md)).

## Find skills

> Find a kubernetes skill in the acme organization.

```sh
gskill --no-interactive --json search kubernetes --owner acme
gskill --no-interactive --json search kubernetes --source github.com/acme/skills
```

- **Changes**: nothing.
- **Result**: a JSON array; each item has `id`, `description`, `source`, `repo_path`, and
  `installed`. Without `--owner` or `--source`, `search` looks in the repositories configured
  for this user.

## List the skills in a source

> What skills does github.com/acme/skills have?

```sh
gskill --no-interactive --json add github.com/acme/skills --list
```

- **Changes**: nothing.
- **Result**: a JSON array with `id`, `description`, `repo_path`, and `valid` per skill.
- Run this before adding from a source with several skills, show the list, and ask which ones
  to add. Pass `--all` only when the user asked for every skill.

## Add a skill

> Add the kubernetes skill from github.com/acme/skills for Claude.

```sh
gskill --no-interactive --json add github.com/acme/skills --skill kubernetes --agent claude
```

- **Version policy**: nothing extra means the newest release tag; `--version '^1.2.0'` tracks a
  range; `--ref v1.2.0` pins a tag or follows a branch; `--commit <sha>` pins a commit. Use what
  the user asked for.
- **Selecting**: `--skill` is repeatable. A path after the repository selects one skill directly:
  `gskill --no-interactive add github.com/acme/skills/skills/kubernetes --agent claude`.
- **Scope**: project by default. `--global` installs for this user in every project, outside
  the repository.
- **Changes**: `skills.toml` gains `[skills.<name>]`; the lockfile gains the entry; content is
  written to `.agents/skills/<name>/` with links such as `.claude/skills/<name>`.
- **Result**: `installed` lists each skill with `name`, `content_hash`, and `targets` (agent to
  path); `warnings` lists advisories.
- **Already declared**: `add` fails and points to `update` or `--force`. Pass `--force` only if
  the user wants to replace the existing declaration.

## Change which agents get a skill

> Also give the kubernetes skill to Codex.

1. Edit `agents` in the skill's `[skills.<name>]` table in `skills.toml`, for example
   `agents = ["claude", "codex"]`.
2. Run:

   ```sh
   gskill --no-interactive --json install
   ```

- **Why not `--agent`**: for a skill that is already declared, `--agent` on `add` or `install`
  changes only the lockfile, and the next plain `install` restores the agents in `skills.toml`.
- **Changes**: `skills.toml` (your edit), the lockfile, and the agent links.
- **Confirm**: `gskill --no-interactive --json info <name>` lists both agents under `agents`.

## Install what the project declares

> Set up the skills this project declares.

```sh
gskill --no-interactive --json install
```

- **Changes**: the lockfile and content, for anything declared but not yet resolved or present.
  Never edits `skills.toml`.
- **Needs** an existing `skills-lock.json`; without one it exits 3. In a new project, start with
  `add`.
- **Result**: `status` (`success` or a failure), `skills` with each skill's `status`, and
  `summary` counts.

## Restore exactly from the lockfile

> Reproduce the locked skills on this machine.

```sh
gskill --no-interactive --json install --frozen-lockfile
```

- **Changes**: content only. The lockfile is never modified.
- **Fails closed**: exit 4 when the lockfile would have to change (for example `skills.toml` was
  edited since it was written); exit 3 when there is no lockfile.

## Show what is installed

> Which skills does this project have?

```sh
gskill --no-interactive --json list
```

- **Changes**: nothing.
- **Result**: `skills`, each with `name`, `version`, `agents`, `status`, `source`, and
  `agent_health` per agent.

## Show one skill

> Tell me about the kubernetes skill.

```sh
gskill --no-interactive --json info kubernetes
```

- **Changes**: nothing.
- **Result**: `name`, `version`, `description`, `source`, `commit`, `agents`, `targets`,
  `requires`, and `overridden`. Exit 2 when the skill is not installed.

## Check for updates

> Are any skills out of date?

```sh
gskill --no-interactive --json update --list
gskill --no-interactive --json update --list --all
```

- **Changes**: nothing.
- **Result**: `updates_available`, `pinned`, and `skills`. Each skill has `name`, `current`,
  `candidate`, `policy`, `status`, `pinned`, `repository`, and `next_action`. `--all` also lists
  skills that are up to date, pinned, or local.
- `status` `no-compatible-update` with a `next_action` such as `gskill upgrade demo` means newer
  releases exist outside the declared range. Report it; do not upgrade unless asked.

## Apply compatible updates

> Update my skills.

```sh
gskill --no-interactive --json update
gskill --no-interactive --json update kubernetes
gskill --no-interactive --json update --repo acme/skills
```

- With `--no-interactive` and no names, `update` applies every available update. Name skills or
  a `--repo` to limit it.
- **Changes**: the lockfile and content. Never `skills.toml`, so a pinned skill stays pinned and
  is reported as `pinned` with its `next_action`.
- **Result**: `updated`, `no_change`, `pinned`, `failed`, and `skills` with `from`, `to`,
  `outcome`, and `status` per skill.

## Move a skill to another version

> Move code-review to 2.1.0.

```sh
gskill --no-interactive --json upgrade code-review --to 2.1.0
gskill --no-interactive --json upgrade code-review --latest
```

- Only when the user asks to change the version. `update` never does this.
- **Changes**: the declaration in `skills.toml`, the lockfile, and content. The declaration keeps
  its shape: `^1.0.0` becomes `^2.1.0`.
- **Result**: per skill `from`, `to`, `outcome`, `declaration_before`, and `declaration_after`.
- `upgrade --all` moves every upgradable skill. Use it only when the user asked for all.

## Verify integrity

> Check that nothing was tampered with.

```sh
gskill --no-interactive --json project verify
```

- **Changes**: nothing. It re-hashes installed content against the lockfile.
- **Result**: `ok`, and `skills` with `name`, `ok`, and `issue`. Exit 6 on any mismatch.

## Check drift

```sh
gskill --no-interactive --json project check
gskill --no-interactive project check --fail-on-drift
```

- **Changes**: nothing. Faster than `verify`; compares metadata, not content.
- **Result**: `has_drift`, `advisories`, and `skills` with `status`. `--fail-on-drift` exits 7 on
  drift.

## Repair

```sh
gskill --no-interactive project repair
gskill --no-interactive project sync
```

- `repair` rewrites broken or edited content to match the lockfile. That discards local edits to
  installed skills, so ask first and say which skills it will touch.
- `sync` installs anything the lockfile records that is missing. `sync --prune` also deletes
  GSKILL-managed installs the lockfile no longer lists; ask first.

## Check the environment

```sh
gskill --no-interactive doctor
```

Reports `git`, detected agents, and each skill's declared requirements, with a fix per warning.

## Remove a skill

> Remove the hello-gskill skill.

```sh
gskill --no-interactive --json remove hello-gskill --force
```

- `--force` is required without a terminal; pass it only because the user asked for this
  removal. Without it, the command exits 1 with "remove requires confirmation".
- **Changes**: the declaration in `skills.toml` (the file is deleted when it was the last skill),
  the lockfile entry, the content, and its agent links.
- **Result**: `removed` and `not_present` name lists.

## Customized skills

A skill can carry an `[skills.<name>.override]` table in `skills.toml` that patches or replaces
upstream files. `update` and `upgrade` reapply it on the new version. Never edit or delete the
override files or the table yourself; if an update reports an override that no longer applies,
show the error and ask.

## After a change

For a project install, tell the user what to commit: `skills.toml`, `skills-lock.json`,
`.agents/skills/<name>/`, and each agent link (for example `.claude/skills/<name>`). After a
project install for `hermes`, the user runs `hermes skills trust` once.
