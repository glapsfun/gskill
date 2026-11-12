# Reading results

Read the exit code first. Then read the JSON on stdout (with `--json`) for per-skill detail. On
an error, stderr holds `error: <message>` and often a `→ <hint>` line; quote both to the user.
Some failures still print JSON on stdout, such as `project verify` (exit 6),
`project check --fail-on-drift` (exit 7), and a partial install (exit 10); read it when present.

## Exit codes

| Code | What to report | Next step |
| --- | --- | --- |
| 0 | Success. Check the JSON: `changed: false`, `no_change`, or `up_to_date` counts mean nothing needed to change. | Report what changed, or that it was already current. |
| 1 | Unexpected error, or a refusal such as "remove requires confirmation". | Quote stderr. For a refusal, follow its hint only if the user asked for that action. Otherwise run `gskill --no-interactive doctor`. |
| 2 | Usage error: a bad flag or argument, or a skill that is not installed. | Fix the command. Never retry with `--force` to get past it. |
| 3 | The lockfile is missing or invalid, or `add` named a skill that is already declared. | Missing: `install` and `project sync` need it. Check that this is the project root and the lockfile is checked out; in a new project, `add` a first skill. Already declared: report it and offer `update`; pass `--force` only if the user wants to replace the declaration. Invalid: show the error; do not delete the file. |
| 4 | A frozen restore would change the lockfile. | Report the mismatch. Running `install` without `--frozen-lockfile` rewrites the lock, so ask first. |
| 5 | Source unavailable or network failure. | Check the source spelling and network access. `--offline` uses only the cache. |
| 6 | Integrity failure: installed content does not match the lockfile. | Stop and report the skills named. `gskill --no-interactive project repair` restores lock-true content and discards local edits, so ask first. Never use `install --force` unasked. |
| 7 | Drift detected (`project check --fail-on-drift`). | Report the skills from `gskill --no-interactive --json project check`. Offer `project repair` or `project sync`, after asking. |
| 8 | An update is available (`outdated --exit-code`), or `self upgrade` printed an upgrade command instead of upgrading. | Report the versions or the printed command. Nothing was changed. |
| 9 | An agent ID GSKILL does not support was named. | Ask which agent to target and pass `--agent <id>`. IDs: `claude`, `codex`, `cursor`, `antigravity`, `opencode`, `openclaw`, `hermes`. |
| 10 | Partial installation: some skills failed. | List which skills succeeded and which failed, from the JSON. Retry only the failed ones after fixing their cause. |
| 11 | Authentication failure. | The user must set up git credentials for that host. Do not ask for tokens in the chat. |
| 12 | Cache or lock failure, including another gskill run holding the lock. | Wait for the other run to finish, then retry once. |
| 130 | Cancelled before approval; nothing was written. | Ask the user how to proceed. |

## JSON fields

Each command's fields are listed with it in [workflows](workflows.md). The ones that decide the
report:

- `add`: `installed[].name`, `installed[].targets`, `warnings`.
- `install`: `status`, `skills[].status`, `summary`.
- `list`: `skills[].name`, `skills[].version`, `skills[].agents`, `skills[].status`.
- `info`: `version`, `agents`, `targets`, `overridden`.
- `update --list`: `skills[].status`, `skills[].current`, `skills[].candidate`,
  `skills[].pinned`, `skills[].next_action`.
- `outdated`: `skills[].status`, `skills[].current`, `skills[].latest`.
- `update`: `updated`, `no_change`, `pinned`, `failed`, `skills[].outcome`.
- `upgrade`: `skills[].outcome`, `skills[].from`, `skills[].to`, `skills[].declaration_after`.
- `project verify`: `ok`, `skills[].issue`.
- `project check`: `has_drift`, `skills[].status`.
- `remove`: `removed`, `not_present`.
- `version`: `version`, `commit`.
