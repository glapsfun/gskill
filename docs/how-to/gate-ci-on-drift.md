# Gate CI on drift

Make a CI job fail when installed skills drift from the lockfile, or when newer skill versions are
available. GSKILL is non-interactive by default, so these commands are CI-safe out of the box.

## Before you start

- A committed `gskill.toml` and `gskill.lock`.
- A CI job that checks out the repo and has `gskill` available.

## Recipe 1 — Fail on drift

```bash
gskill install --frozen-lockfile     # restore exactly; exit 4 if lock is out of sync
gskill check --fail-on-drift         # exit 7 if installed state drifts from the lock
```

**Expected:** both exit `0` on a clean, in-sync project. `check --fail-on-drift` exits **`7`** if drift
is detected; `install --frozen-lockfile` exits **`4`** if the manifest and lockfile disagree.

## Recipe 2 — Fail when updates are available

```bash
gskill outdated --exit-code          # exit 8 if any skill has a newer version
```

**Expected:** exit `0` when everything is current; exit **`8`** when at least one update is available.
Use this in a scheduled job to be notified, not in the main build if you don't want updates to break it.

## Reading results in scripts

```bash
gskill check --json                  # structured status on stdout
```

Diagnostics go to **stderr**; the `--json` result is the only thing on **stdout**, so it stays
parseable.

## See also

- [Reproduce with --frozen-lockfile](reproduce-with-frozen-lockfile.md)
- [Script GSKILL with --json](script-with-json.md)
- [Exit codes](../reference/exit-codes.md)
