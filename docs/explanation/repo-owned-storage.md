# Repo-owned storage

GSKILL stores skill content **inside the repository** and commits it. The repo is the single source
of truth for what a project's skills contain; your home directory holds only a download cache. This
page explains the model and its consequences.

## Committed content, committed links

Each installed skill is a real directory at `.agents/skills/<name>/`, committed to the repository.
Agent directories (`.claude/skills/<name>`, `.codex/skills/<name>`, `.cursor/skills/<name>`,
`.gemini/skills/<name>`) hold committed **relative symlinks** into it (e.g.
`../../.agents/skills/<name>`), so every agent sees the same bytes without duplication.

The `.gitignore` gskill manages contains only a `.gskill/` line — `.agents/` is **not** ignored,
because the content and links are meant to be committed.

## Clone and go

Because content and links are committed, a fresh `git clone` yields working skills with **zero
gskill commands**. Teammates and CI need nothing installed — no restore step, no bootstrap, no
gskill binary — unless they want to add, update, or verify skills.

## The home clone cache

`$HOME/.gskill` (relocatable only via `GSKILL_HOME`) contains only `cache/` (commit-keyed git
clones at `cache/<40-hex-commit>/`), `locks/`, `tmp/`, and `config.toml`. It is a **pure
performance cache**, shared by every project on the machine: nothing in the repo ever references
it, and deleting it just causes re-fetches. See [the clone cache](store-and-cache.md).

## Restore order: committed → cache → fetch

Reproduction comes from `skills-lock.json` alone. `gskill install` works through three layers,
cheapest first:

1. **Committed content** whose hash matches the lock is up to date — zero network.
2. **Missing content** is restored from the clone cache by the recorded commit.
3. A **cold cache** fetches exactly once from the recorded source.

`install --frozen-lockfile` never mutates the lock and fails closed.

## Drift is reported, never auto-repaired

Hand-editing committed skill content makes plain `add`/`install`/`sync` fail with
"committed content for skill X ... no longer matches skills-lock.json", plus a hint: run
`gskill project repair` (or `gskill install --force`) to restore lock-true content, or re-add the skill to
adopt the edits. `gskill project check --fail-on-drift` exits `7` on it, so CI catches drift too.

## Migrating from the old model

There is no migrate command. Any mutating command (`add`, `install`, `update`, `sync`, `remove`)
transparently converts a legacy (pre-022) project: content is copied into `.agents/skills/`, links
become relative, and lock entries drop their `scope`/`storeHash` fields — announced by exactly one
notice line: `migrated N skill(s) to repo-owned storage; the old store at PATH was left untouched`.
The old `~/.gskill/store` is never modified or deleted; delete it by hand once no other machine
needs it.

## Platform requirement

The model needs working symlinks: macOS and Linux are supported; Windows is not. A checkout made
with `core.symlinks=false` leaves plain files where agent links should be — `gskill project check` and
`gskill doctor` report it, and the fix is to re-clone on a symlink-capable filesystem. `--copy`
remains a user-chosen install mode (real copies instead of links).

## The trade-off

Repos grow by the size of their vendored skills, and there is no cross-project content dedup
anymore. In exchange, clones just work, and the clone cache still prevents re-downloads — the
expensive part.

## See also

- [The clone cache](store-and-cache.md)
- [Project state vs global state](project-and-global-state.md)
- [The reproducibility model](reproducibility-model.md)
