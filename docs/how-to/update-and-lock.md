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

**Expected:** a read-only report on stdout, grouped by the repository each skill comes from.
Nothing is installed and no file changes:

```text
github.com/acme/skills · 1 update available / 3 skills
NAME        CURRENT  AVAILABLE  POLICY
kubernetes  1.2.0    1.4.1      ^1.2.0

1 update available · 1 up to date · 2 pinned (--all shows them)
```

Without `--all`, a repository with nothing to update is left out, but the summary still counts
every skill, so an empty report is explained. Add `--all` to see every repository and the
skills that cannot move, with the reason:

```bash
gskill update --list --all
```

```text
../tools · 0 updates available / 1 skill
NAME         CURRENT  AVAILABLE  POLICY  STATUS
local-notes  local    --         local   local source

github.com/acme/skills · 1 update available / 3 skills
NAME         CURRENT  AVAILABLE  POLICY  STATUS
code-review  1.2.0    --         1.2.0   pinned version
  pinned by skills.toml (version = "1.2.0"); newest 2.1.0. Run 'gskill upgrade code-review' or edit skills.toml to move it.
kubernetes   1.2.0    1.4.1      ^1.2.0  update available
security     2.3.0    --         ^2.0.0  up to date (newer 3.0.0 outside ^2.0.0)
  newer releases exist outside the version constraint. Run 'gskill upgrade security --latest' to move to 3.0.0.

1 update available · 1 up to date · 2 pinned
```

A group line adds `· N failed` or `· N not checked` when a lookup failed or was skipped.
Groups are sorted by label, so local paths come first, and skills are sorted by name inside
each group.

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

In a terminal, run `gskill update` with no arguments. It opens a table of every tracked skill,
grouped by repository. Only a skill with an update available has a checkbox; the others show
their status, so nothing is hidden:

```text
Select updates (space toggles, enter reviews):

  ▸ ../tools · 0 updates available / 1 skill · local
❯ ▾ github.com/acme/skills · 1 update available / 3 skills · 1 selected
      code-review  1.2.0          1.2.0   pinned version
  [✓] kubernetes   1.2.0 → 1.4.1  ^1.2.0  update available
      security     2.3.0          ^2.0.0  up to date (newer 3.0.0 outside ^2.0.0)
  ▾ github.com/example/devops · 1 update available / 1 skill
  [ ] deploy       0.3.0 → 0.4.0  ^0.3.0  update available

github.com/acme/skills · 1 update available / 3 skills
2/7 · 1 selected  •  ↑/↓ move · space toggle · a all · ←/→ fold · / filter · enter review · esc cancel
```

The line above the footer explains the row under the cursor: why a skill cannot move and the
command that moves it, or the group's counts on a header.

| Key | Action |
| --- | --- |
| `↑` `↓` `k` `j` | move over group headers and skills |
| space, `x` | on a skill: toggle it. On a group header: select every skill in the group that can update, or clear them if all are selected |
| `a` | the same for every skill in the project |
| `←` `h` | fold the group (on a skill, jump to its header) |
| `→` `l` | unfold the group |
| `/` | filter by skill name, path, or repository. While a filter is set, space on a header and `a` act only on the skills it matches |
| enter | review the selection |
| esc | leave the filter input, then clear the filter, then cancel |
| `q` | cancel |
| Ctrl-C | interrupt |

So updating one repository takes three keys from its header: space, enter, enter. The footer
counts selected skills that a filter or a fold hides.

Enter opens a review of exactly what will change, grouped the same way. It is the only
confirmation:

```text
Review 2 updates:

github.com/acme/skills
  kubernetes   1.2.0 → 1.4.1
github.com/example/devops
  deploy       0.3.0 → 0.4.0

enter apply · esc cancel
```

Enter applies; esc or `q` cancels and leaves without writing anything. After the run, the
grouped result prints and the table opens again with fresh statuses, so you can update another
repository in the same session. Leave with `q` or esc.

Leaving exits `0`, or `10` if a run in the session ended with a failed skill. Enter with
nothing selected prints `Nothing selected; no updates applied.` and exits `0`. Ctrl-C exits
`130`.

## Update one repository

Pass `--repo` to update the skills from one repository without the table or a prompt:

```bash
gskill update --repo github.com/acme/skills
gskill update --repo acme/skills --repo ../tools   # repeat it for more than one
gskill update --list --repo acme/skills            # report only, scoped to that repository
gskill update kubernetes --repo acme/skills        # only the named skills from it
```

Any spelling of the same repository selects the same skills: `acme/skills`,
`github.com/acme/skills`, `https://github.com/acme/skills.git`, and
`git@github.com:acme/skills.git`. On github.com, gitlab.com, bitbucket.org, and codeberg.org,
owner and repository names match regardless of case. A local path is resolved against the
project root. The group label that `update --list` prints, and the `repository` field in
`--json` output, can always be passed back to `--repo`.

These fail with exit `2` before any network call, and write nothing:

```text
$ gskill update --repo acme/other
error: no tracked skill comes from "acme/other"
→ repositories in this project: ../tools, github.com/acme/skills, github.com/example/devops

$ gskill update --repo acme/skills/kubernetes
error: --repo "acme/skills/kubernetes" names a path inside a repository
→ use --repo github.com/acme/skills and select skills by name

$ gskill update deploy --repo acme/skills
error: skill "deploy" comes from github.com/example/devops, not from the selected repository
```

## Update directly

```bash
gskill update kubernetes         # update only the named skill
gskill --no-interactive update   # update every available skill, no prompt
```

**Expected:** each processed skill reports the revision it moved from and to, grouped by
repository:

```text
github.com/acme/skills · 1 updated / 2 skills
NAME         FROM   TO     RESULT
code-review  1.2.0  1.2.0  pinned version
kubernetes   1.2.0  1.4.1  updated

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

The JSON items carry `status`, `shape`, `pinned`, `next_action`, `repository`, and `local`, so a
script can branch or group without parsing the human text.
