# Script GSKILL with `--json`

Drive GSKILL from scripts and agents by consuming machine-readable output and branching on exit codes.

## Before you start

- A project with at least one installed skill.

## The output contract

- **stdout** carries the primary result only. With `--json`, that is a single stable JSON object.
- **stderr** carries progress, warnings, and diagnostics — never mixed into the JSON on stdout.
- When there is no TTY (as in CI), GSKILL is automatically non-interactive: no colours, spinners, or
  prompts.

`--json` is available for the status commands a script is most likely to consume, including `list`,
`check`, `outdated`, and `verify`.

## Steps

```bash
gskill list --json        # inventory of installed skills
gskill project check --json       # fast drift status
gskill outdated --json    # available updates per skill
gskill project verify --json      # integrity result
```

Parse stdout as JSON and branch on the exit code:

```bash
if gskill project check --json > status.json; then
  echo "in sync"
else
  echo "drift or error (exit $?)"   # see the exit-codes reference
fi
```

## Expected result

- Each command prints one JSON object to stdout and exits `0` on success.
- Exit codes are stable and documented — branch on them rather than parsing human text. See
  [exit codes](../reference/exit-codes.md).

## Group skills by repository

Each skill object in `outdated --json`, `update --list --json`, and `update --json` carries
two fields about where it comes from:

- `repository`: the repository label that `update --list` groups the skill under. Pass it to
  `gskill update --repo` to act on that repository.
- `local`: `true` when the source is a local path.

With `--all`, `update --list` reports every tracked skill, not only the ones that can update.
This lists the available updates per repository:

```bash
gskill --json update --list --all \
  | jq -c '.skills | group_by(.repository) | map({repository: .[0].repository, updates: map(select(.status == "update-available") | .name)})'
```

```json
[{"repository":"../tools","updates":[]},{"repository":"github.com/acme/skills","updates":["kubernetes"]}]
```

## See also

- [Gate CI on drift](gate-ci-on-drift.md)
- [Command reference](../reference/commands.md)
