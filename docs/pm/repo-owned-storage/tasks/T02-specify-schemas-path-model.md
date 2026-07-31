---
id: T02
epic: repo-owned-storage
milestone: M1
title: Specify the repo-owned schemas and path model
status: todo
priority: must
depends-on: [T01]
estimate: M
owner: unassigned
updated: 2026-07-31
---

# T02 — Specify the repo-owned schemas and path model

## Context

Today's records assume a store: the lock's `gskill` extension carries
`storeHash` and `scope` (`internal/skillslock/gskillext.go:8-23`),
`state.json` carries `StoreHash`/`StoreScope` per skill
(`internal/projstate/state.go:42-54`), and recorded targets are
repo-relative only for project scope (`internal/installer/installer.go:496-505`).
Before code moves, this task writes the single authoritative definition of
what is recorded where in the repo-owned model, honoring the spec 012
constraint that tool-shared core lock fields stay untouched
([../epic.md](../epic.md) Non-goals).

## What to do

- Write a short design note (internal location per project convention:
  `.docs/`) defining:
  - Lock `gskill` extension after the change: fields dropped (`storeHash`,
    `scope`), fields kept (`sourceUrl, ref, commit, version, agents,
    installMode, skillFileHash, installedAt, updatedAt, state`), and the
    rule that every recorded path is repo-root-relative with `/` separators.
  - `state.json` after the change: what remains once targets are committed
    (candidates to drop: `StoreHash`, `StoreScope`, `ActiveTarget` if
    derivable), and whether the file survives at all vs. deriving state from
    the tree + lock.
  - Home layout contract: `cache/` (commit-keyed), `locks/`, `tmp/`,
    `config.toml` only; name the config keys that die (`storeScope`,
    `storeVerifyOnUse`, `storeGCGracePeriod`, `projectsRegistry`,
    `privacy.projectRegistry`).
  - Content-integrity rule: committed content is verified against the lock's
    `computedHash` by `install`/`check`; define the drift error and hint.
- Review the note against `specs/012-skills-lock-compat` to confirm no core
  field changes.

## Acceptance criteria

- [ ] Design note exists in `.docs/` covering all four bullets with concrete
      field lists.
- [ ] Explicit statement, with field-by-field diff, that `skills-lock.json`
      core fields are byte-compatible with spec 012.
- [ ] T03/T04 implementers need no further schema decisions (checked by the
      absence of TODO/TBD markers in the note).

## Out of scope

- Implementing any schema change — this is specification only.
- Migration mechanics for old lock entries (T07 consumes this note).

## Notes

Struct sources: `internal/skillslock/record.go:7-97`,
`internal/projstate/state.go:32-54`, `internal/home/home.go:65-86`,
`internal/config/config.go:31-73`.
