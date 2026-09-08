# Upgrade a skill

Move what `skills.toml` declares for a skill, then resolve, lock, install, and verify it in
one command. `upgrade` is the only command besides `add` and `remove` that edits
`skills.toml`; `update` never does. See [the package lifecycle](../explanation/lifecycle.md)
for how the two relate.

## Before you start

- A committed `skills.toml` and `skills-lock.json`.
- Network access: `upgrade` lists the source's releases to find a candidate.

## Move to the newest release

```bash
gskill upgrade code-review --latest      # --latest is the default
```

**Expected:**

```text
NAME          FROM     TO      DECLARATION            RESULT
code-review   1.2.0    2.1.0   ^1.0.0 → ^2.0.0        upgraded

Upgraded 1 skill; skills.toml and skills-lock.json updated; verified
```

Exactly one line of `skills.toml` changed, and the declaration kept its shape: a caret
range stays a caret range (`^1.0.0` → `^2.0.0`), an exact version stays exact
(`1.2.0` → `2.1.0`), a tag stays a tag, a commit stays a commit. Pre-releases are skipped
unless the declaration already names one.

If the newest release already satisfies the current range, nothing is rewritten and only the
lock moves, exactly as `update` would; the result reads `updated`.

## Move to a specific version

```bash
gskill upgrade code-review 2.1.0         # positional form, one skill only
gskill upgrade code-review --to 2.1.0    # the same, explicit
```

The target must exist as a release of the skill's source; otherwise the command refuses with
exit `2` and writes nothing. The declaration keeps its shape, so for a caret range the target
becomes the new floor (`^2.1.0`) and the lock resolves to the newest release that range allows;
for an exact version the target is installed as written. A result that lands below the locked
version is labelled `downgraded`, one that moves the declaration without changing the resolved
revision is labelled `redeclared`.

## Upgrade everything

```bash
gskill upgrade                           # in a terminal: pick from a multi-select
gskill --no-interactive upgrade --all    # CI: move every upgradable skill
```

Without skill names outside a terminal, `upgrade` refuses unless `--all` is given: rewriting
intent for every skill is the one lifecycle action a re-run cannot undo.

## Preview

```bash
gskill --dry-run upgrade code-review
```

Prints the declaration change and the version it would install; nothing is written.

## What cannot be upgraded

| Declaration | Why | Do this instead |
| --- | --- | --- |
| `ref = "main"` (a branch) | no version to move | `gskill update code-review` |
| a local path | no releases | edit `skills.toml` |
| `version = ">=1.0.0 <3.0.0"` | shape cannot be preserved mechanically | edit `skills.toml`, then `gskill install` |
| `--offline` | releases cannot be listed | drop `--offline` |
| `--to <tag>` where the tag is not semver, against a `version = ...` declaration | a version key cannot hold a non-semver tag, and writing an empty one would erase the pin | declare `ref = "<tag>"` instead, then `gskill install` |

Each refusal exits `2` with a hint and touches nothing.

## Failure and rollback

`upgrade` snapshots `skills.toml` and `skills-lock.json` before writing. If resolution,
install, or verification fails, or the run is interrupted, both files are restored and the
installed content is reconciled back from the restored lock, so the three layers never
disagree. The exit code is that of the failure (`130` for an interrupt).

The restore replaces only content this run installed. Content you edited by hand is never
overwritten by a rollback: the restore stops and the run reports `rollback incomplete` with a
hint to run `gskill project repair`, so a project that no longer matches its lockfile always says so
rather than failing quietly.

## Machine-readable output

```bash
gskill --json upgrade code-review
```

One object with `upgraded`, `unchanged`, `refused`, `failed`, and per-skill `shape`,
`declaration_before`, `declaration_after`, `action`, and `outcome`.
