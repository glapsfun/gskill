---
id: T02
epic: cli-surface-cleanup
milestone: M2
title: Prune unreachable command ownership safely
status: todo
priority: must
depends-on: [T01]
estimate: L
owner: unassigned
updated: 2026-07-29
---

# T02 — Prune unreachable command ownership safely

## Context

Removing public commands must not accidentally remove behavior that the
remaining workflow still needs. In particular, `search` calls `App.SourceList`
([`internal/app/find.go`](../../../../internal/app/find.go)), and install and
onboarding call `App.Init`; those are not removable. By contrast,
`SourceInspect`, `SourceCheck`, and `App.Unlink` are dedicated to the retired
CLI paths. This task eliminates only the latter ownership paths and brings
tests onto the retained workflows described in the [epic](../epic.md).

## What to do

- Trace all production and test callers of `App.Init`, `App.SourceList`,
  `SourceInspect`, `SourceCheck`, and `App.Unlink` after T01.
- Retain and test internal auto-initialization and search source listing.
- Remove unreachable source-inspection/check and unlink result, helper, and
  test code; update integration fixtures that invoked public `init` or
  `unlink` to exercise supported setup and agent-narrowing/removal workflows.
- Preserve existing safety guarantees for `install --agent`, `remove`, stores,
  and active entries; do not silently recreate the old retained-unreferenced
  behavior under another command.

## Acceptance criteria

- [ ] Production code contains no reference to `App.Unlink`, `SourceInspect`,
  or `SourceCheck`.
- [ ] `App.Init` remains available to installation/onboarding, and a
  previously uninitialized project can still be prepared by `add` or `install`.
- [ ] `gskill search` remains able to enumerate source content through
  `App.SourceList`.
- [ ] Focused application and integration tests cover auto-init, search, and
  exact-agent install/removal behavior without invoking retired commands.

## Out of scope

- Changing source discovery, cache, lockfile, store, or agent-adapter behavior.
- Retaining a zero-agent, unreferenced lock entry as a compatibility feature.
- Editing generated documentation; that is T03.

## Notes

- `source list` is partly overlapped by `add <source> --list`; its retained
  lower-level source listing is also required by search.
- `install --agent` takes the exact desired agent set; removing all agents is
  a removal operation, not a new empty-agent CLI syntax.
