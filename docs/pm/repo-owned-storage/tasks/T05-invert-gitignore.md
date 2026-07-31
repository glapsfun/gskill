---
id: T05
epic: repo-owned-storage
milestone: M2
title: Stop gitignoring .agents/ so skill content and links are committable
status: todo
priority: must
depends-on: [T03]
estimate: S
owner: unassigned
updated: 2026-07-31
---

# T05 — Stop gitignoring `.agents/` so skill content and links are committable

## Context

Spec 017 made gskill append `.gskill/` and `.agents/` to `.gitignore`
(`internal/app/install.go:1070`, `ensureGitignore:1074`). The epic's primary
metric — clone equals working skills — is impossible while `.agents/` is
ignored. This task reverses that half of the decision: `.agents/` becomes
committed content, `.gskill/` (local state) stays ignored.

## What to do

- Change `gskillIgnorePatterns` to `.gskill/` only.
- On mutating commands, if a *gskill-written* `.agents/` ignore line is
  present, remove it (recognize the managed block/line exactly; never touch
  user-authored lines) so existing projects become committable after
  migration.
- Update the integration tests around gitignore behavior
  (`test/integration/…gitignore…`, `internal/app/gitignore_test.go`).
- Decide nothing about agent dirs (`.claude/` etc.): gskill has never
  ignored those and does not start now.

## Acceptance criteria

- [ ] Fresh `add` in a clean repo leaves `.agents/` unignored and
      `git status` shows the skill copy and agent links as addable.
- [ ] `.gskill/` remains ignored.
- [ ] A pre-existing gskill-written `.agents/` ignore line is removed on the
      next mutating command; a user-authored `.agents/` line is left alone
      (both cases tested).
- [ ] `./scripts/verify.sh` exits 0.

## Out of scope

- Migration of link targets themselves (T07).
- Documentation of the new committed layout (T08).
