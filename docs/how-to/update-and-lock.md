# Update and re-lock

Review which skills can move to a newer version, choose what to update, and recompute the
lockfile. `update` re-resolves the intent declared in `skills.toml` and never edits that file;
to change what a skill is allowed to move to, see [Upgrade a skill](upgrade-a-skill.md).

## Before you start

- A committed `skills.toml` and `skills-lock.json`.

## See what an update would do

```bash
gskill update --list
```

**Expected:** a read-only report on stdout — nothing is installed and no file changes:

```text
NAME          CURRENT   AVAILABLE  POLICY
kubernetes    1.2.0     1.4.1      ^1.2.0

1 update available · 1 up to date · 2 pinned (--all shows them)
```

The summary counts every skill, so an empty table is explained. Add `--all` to see the
skills that cannot move and why:

```bash
gskill update --list --all
```

```text
NAME          CURRENT   AVAILABLE  POLICY   STATUS
code-review   1.2.0     --         1.2.0    pinned version
  pinned by skills.toml (version = "1.2.0"); newest 2.1.0. Run 'gskill upgrade code-review' or edit skills.toml to move it.
security      2.3.0     --         ^2.0.0   up to date (newer 3.0.0 outside ^2.0.0)
  newer releases exist outside the version constraint. Run 'gskill upgrade security --latest' to move to 3.0.0.
local-notes   local     --         local    local source
```

Every skill is in exactly one of these states:

| Status | Meaning |
| --- | --- |
| `update available` | a newer revision satisfies the declaration |
| `up to date` | nothing newer satisfies the declaration |
| `up to date (newer X outside C)` | a newer release exists beyond the constraint; `upgrade` moves it |
| `pinned version` / `pinned tag` / `pinned commit` | the declaration names one revision; `upgrade` or an edit moves it |
| `local source` | a local path has no releases |
| `lookup failed` | the source could not be reached (`(offline)` when you asked for no network) |

`gskill outdated` renders the same report and additionally supports `--exit-code` (exit `8`
when updates are available) for CI gates. A report with a failed lookup exits `5` after
printing, so an outage never passes as "up to date"; an `--offline` run exits `0`.

## Choose updates interactively

In a terminal, run `gskill update` with no arguments to open a multi-select over the
available updates:

```text
Select updates (space toggles, enter confirms):
  [✓] kubernetes   1.2.0 -> 1.4.1   ^1.2.0
  [ ] security     2.3.0 -> 2.3.2   ^2.0.0
```

Confirm to apply exactly the selection. Cancelling (esc or `q`) changes nothing and exits `0`;
Ctrl-C exits `130`.

## Update directly

```bash
gskill update kubernetes         # update only the named skill
gskill --no-interactive update   # update every available skill, no prompt
```

**Expected:** each processed skill reports the revision it moved from and to:

```text
NAME          FROM      TO       RESULT
code-review   1.2.0     1.2.0    pinned version
kubernetes    1.2.0     1.4.1    updated

  code-review: pinned by skills.toml (version = "1.2.0"); newest 2.1.0. Run 'gskill upgrade code-review' or edit skills.toml to move it.

Updated 1 skill · 1 pinned
```

A pinned or up-to-date skill is an honest no-op: the run exits `0`, `skills.toml` is
byte-identical, and the guidance line says how to move it. If one skill fails mid-run, the
others still update and the command exits `10`.

## Edited `skills.toml` by hand?

`update` reads the declaration as it is now. A changed constraint is reported as
`declaration changed in skills.toml` and applied by the next `update` or `install`; no
intervening command is needed.

## Preview and automation

```bash
gskill --dry-run update                # report would-be transitions, write nothing
gskill --json update --list            # machine-readable report on stdout
gskill --offline update --list --all   # classify pins/locals without the network
```

The JSON items carry `status`, `shape`, `pinned`, and `next_action`, so a script can branch
without parsing the human text.
