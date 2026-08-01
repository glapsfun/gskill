---
id: T03
epic: repo-owned-storage
milestone: M2
title: Re-key activation and ownership predicates to .agents/skills
status: todo
priority: must
depends-on: [T02]
estimate: L
owner: unassigned
updated: 2026-08-01
---

# T03 — Re-key activation and ownership predicates to `.agents/skills`

## Context

The activation layer creates absolute symlinks into a store
(`internal/active/active.go:118-134`, `internal/installer/installer.go:539`),
and every safety predicate that decides "is this link ours to modify" keys on
store roots: `active.Owned` (`active.go:173`), `guardForeignTarget`
(`installer.go:462-484`), `managedRoots` (`internal/app/sync.go:422-439`),
`checkSafeTargetRemoval` (`internal/app/paths.go:72`). This task inverts the
foundation: `.agents/skills/<name>` becomes a real copied directory (the
source of truth), agent links become relative, and ownership is decided
against the repo's `.agents/skills` root. Everything in T04 sits on this.

## What to do

- `.agents/skills/<name>`: installer copies skill content here (from the
  clone-cache materialization) instead of symlinking into a store; content
  hash verified after copy against the lock's expected hash.
- Agent links: `activateAgent` creates symlinks whose stored target is
  *relative* (e.g. `../../.agents/skills/<name>`); the user-chosen `--copy`
  mode is unchanged in spirit but sourced from the repo copy. Link-health
  checks report T01's specified error for a symlink-less checkout (no
  auto-reconciliation — Windows is out of scope).
- Re-key `Owned`, `HealthOf`, `guardForeignTarget`, `managedRoots`,
  `checkSafeTargetRemoval` to the `.agents/skills` root; foreign links
  (including legacy absolute home-store links) still fail closed — T07
  handles converting them.
- `recordTarget` records repo-relative paths in all cases it still handles.
- Extend the adversarial/foreign-target integration tests
  (`test/integration/adversarial_test.go`, `add_conflict_test.go`) to the
  new root model *first* (test-first gate).

## Acceptance criteria

- [ ] After `add`, `.agents/skills/<name>` is a regular directory whose hash
      matches the lock, and `.claude/skills/<name>` is a symlink whose
      `readlink` output starts with `../` (no absolute paths anywhere).
- [ ] `remove`/`sync` refuse to touch a link pointing outside
      `.agents/skills` (foreign-target tests green).
- [ ] `active.Owned`/`HealthOf` report correctly for: healthy relative link,
      symlink-less-checkout plain file (T01's error), legacy absolute link,
      foreign link.
- [ ] `./scripts/verify.sh` exits 0.

## Out of scope

- The add/install/update/sync flow logic above the installer (T04).
- Gitignore changes (T05); deleting the store packages (T06); migration (T07).

## Notes

Copy-vs-symlink selection: `installer.agentActivation`
(`internal/installer/installer.go:523`), `fsutil.SymlinkOrCopy`
(`internal/fsutil/fsutil.go:79`).
