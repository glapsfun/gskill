---
id: T07
epic: repo-owned-storage
milestone: M4
title: Auto-migrate legacy home-store projects on the next mutating command
status: todo
priority: must
depends-on: [T04, T06]
estimate: M
owner: unassigned
updated: 2026-07-31
---

# T07 — Auto-migrate legacy home-store projects on the next mutating command

## Context

Every existing gskill project has absolute symlinks into `~/.gskill/store`
(or a legacy `<repo>/.gskill/store`) and lock/state records carrying
`storeHash`/`scope`. The user chose transparent migration: any mutating
command (`add`, `install`, `update`, `sync`, `remove`) detects the legacy
layout and converts it, with a one-line notice — no separate command, no
read-only grace period beyond non-mutating commands not touching anything.

## What to do

- Detection: on opening a project for mutation, classify each skill as
  legacy if its active/agent links are absolute into a known store root or
  its lock/state records carry store fields.
- Conversion per skill, in this preference order for content: existing store
  object (old home or legacy project store) if present and hash-valid →
  home clone cache by recorded commit → network fetch by lock source+commit.
  Copy into `.agents/skills/<name>`, verify hash, rewrite links relative,
  rewrite lock extension and state per T02.
- Read-compatibility: lock entries with old fields parse cleanly (spec 012
  lossless parse already preserves unknown keys); old fields are dropped on
  the first rewrite.
- Notice: exactly one line summarizing the migration (N skills migrated to
  repo-owned storage; old store left untouched at <path>).
- Old store data is never deleted or GC'd (epic non-goal).
- Tests: fixture projects in each legacy shape (global-scope links,
  project-scope links, copy-mode installs, missing store object with warm
  cache, cold cache) driven through each mutating command.

## Acceptance criteria

- [ ] Each legacy fixture, after one mutating command, satisfies T04's
      pipeline acceptance checks (repo copy, relative links, clean records)
      and the command's own effect still applies.
- [ ] Migration with a missing store object but warm clone cache completes
      offline; with cold cache it fetches by recorded commit and the result
      hash-matches the lock.
- [ ] The one-line notice appears exactly once per migrating run;
      non-mutating commands (`list`, `check`) leave legacy projects
      untouched but report their status accurately.
- [ ] `~/.gskill/store` contents are byte-identical before and after
      migration; `./scripts/verify.sh` exits 0.

## Out of scope

- A standalone `gskill migrate` command — declined by user decision.
- Cleaning up the orphaned home store — user-owned data.

## Notes

The deleted `internal/migrate` package (T06) solved the inverse direction;
its relink logic (`relinkAll`) is prior art for atomically re-pointing links.
