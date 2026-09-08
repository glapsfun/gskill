# The package lifecycle

GSKILL keeps two committed files. `skills.toml` is the declaration of intent you write:
which skills, which version constraint or pin, which agents, which customizations.
`skills-lock.json` is the record of what that intent resolved to, written by the tool and
never edited by hand. Every command owns exactly one relationship between the two.

| Command | Owns | Touches `skills.toml`? | Touches `skills-lock.json`? |
| --- | --- | --- | --- |
| `add` | introduce new intent and realize it | writes a new entry | writes a new entry |
| `install` | realize declared intent | never | rewrites only entries whose declaration changed |
| `update` | re-resolve existing intent to the newest compatible revision | never | rewrites entries that moved |
| `upgrade` | change existing intent, then realize it | rewrites the named entries | rewrites entries that moved |
| `remove` | retire intent | deletes the entry | deletes the entry |
| `sync` | reconcile disk to the lock | never | never |
| `verify` / `check` | prove disk matches the lock | never | never |

In one line:

```text
upgrade = change intent + resolve + install
update  = re-resolve existing intent
install = realize declared intent
```

## Why `update` never edits the manifest

A declaration such as `version = "^1.0.0"` is a promise about what the project accepts.
`update` keeps that promise: it moves the lock from `1.2.0` to `1.4.0` when `1.4.0` is
published, and stays put when only `2.0.0` is. A declaration such as `version = "1.2.0"`,
an exact tag, or an exact commit accepts one revision only, so `update` reports it as
**pinned** and names the two ways to move it: `gskill upgrade <skill>`, or an edit to
`skills.toml` followed by `gskill install`. A run in which nothing moved exits `0` and
says why, per skill; it is never an error.

## Decision: `upgrade` exists

Editing `skills.toml` by hand and running `gskill install` works and stays supported. The
command exists because that path fails the two audiences GSKILL treats as first-class,
scripts and agents:

- **Discovery.** Nobody can write the right version into the manifest without a command
  that lists what exists beyond the current constraint.
- **Safe mutation.** Moving a constraint by hand means choosing the new operator shape
  (`^1.0.0` becomes `^2.0.0`, not `2.0.0`) and leaving every other byte, including
  comments, untouched. `upgrade` rewrites exactly one key of one declaration and keeps its
  shape: a caret range stays a caret range, an exact version stays exact, a tag stays a
  tag, a commit stays a commit.
- **Atomicity.** A hand edit followed by a failed resolve leaves the manifest ahead of the
  lock. `upgrade` snapshots both files first and, on any failure or interrupt, restores
  the manifest, the lock, and the installed content together.
- **Automation.** "Bump every skill to its latest release and open a pull request" is one
  command with `--all`.

`upgrade` never installs anything `skills.toml` does not declare, refuses declarations it
cannot rewrite mechanically (a branch, a local path, a complex range), and never selects a
pre-release unless the declaration already names one.

## Statuses `update` reports

| Status | Meaning | What moves it |
| --- | --- | --- |
| update available | a newer revision satisfies the declaration | `gskill update` |
| up to date | nothing newer satisfies the declaration | nothing |
| up to date (newer X outside C) | a newer release exists beyond the constraint | `gskill upgrade <skill> --latest` |
| pinned version / tag / commit | the declaration names one revision | `gskill upgrade <skill>` or edit `skills.toml` |
| local source | a local path has no releases | edit `skills.toml` |
| lookup failed | the source could not be reached, or the run was offline | retry with network |

See also [the reproducibility model](reproducibility-model.md) for how the lock records the
intent it resolved from.
