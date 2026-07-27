# Update and re-lock

Review which skills can move to a newer version, choose what to update, and
recompute the lockfile — without ever changing a skill's requested tracking
policy.

## Before you start

- A committed `skills-lock.json`.

## See what an update would do

```bash
gskill update --list
```

**Expected:** a read-only report on stdout — nothing is installed and
`skills-lock.json` is untouched:

```text
NAME          CURRENT   AVAILABLE  POLICY
kubernetes    1.2.0     1.4.1      ^1.2.0
security      2.3.0     2.3.2      ^2.0.0

2 updates available
```

Add `--all` to also see the skills that cannot move and why:

```bash
gskill update --list --all
```

Statuses include `up to date`, `pinned tag`, `pinned commit`, `local source`,
and `no compatible update` (a newer release exists, but your version
constraint forbids it). A skill pinned to an exact tag or commit is **never**
offered as an update — `gskill update` preserves pins.

`gskill outdated` renders the same report and additionally supports
`--exit-code` (exit `8` when updates are available) for CI gates.

## Choose updates interactively

In a terminal, run `gskill update` with no arguments to open a multi-select
over the available updates:

```text
Select updates (space toggles, enter confirms):
  [✓] kubernetes   1.2.0 -> 1.4.1   ^1.2.0
  [ ] security     2.3.0 -> 2.3.2   ^2.0.0
```

Confirm to apply exactly the selection. Cancelling (esc or `q`) changes
nothing and exits `0`; Ctrl-C exits `130`.

## Update directly

```bash
gskill update kubernetes         # update only the named skill
gskill --no-interactive update   # update every available skill, no prompt
```

**Expected:** each processed skill reports the revision it moved from and to:

```text
NAME          FROM      TO       RESULT
kubernetes    1.2.0     1.4.1    updated
```

A named skill that is already up to date (or pinned) reports an honest no-op
and exits `0`. If one skill fails mid-run, the others still update and the
command exits `10`.

## Preview and automation

```bash
gskill --dry-run update                # report would-be transitions, write nothing
gskill --json update --list            # machine-readable report on stdout
gskill --offline update --list --all   # classify pins/locals without the network
```

`--dry-run` never touches the lockfile, the store, or agent directories — in
a terminal it still opens the selector and simulates the confirmed choice.

## Expected result

- `gskill update` may change resolved versions; `gskill install` never does.
- Updates rewrite only the resolved state in `skills-lock.json`; the requested
  version intent (e.g. `^1.2.0`) is preserved. Review the diff before
  committing — it should match your intent.

## See also

- [Add a skill from Git](add-a-git-skill.md)
- [Gate CI on drift](gate-ci-on-drift.md)
- [Use different skill versions](use-different-skill-versions.md)
