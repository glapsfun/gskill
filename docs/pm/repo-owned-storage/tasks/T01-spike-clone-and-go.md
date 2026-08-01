---
id: T01
epic: repo-owned-storage
milestone: M1
title: Spike: prove committed relative links survive git clone-and-go
status: todo
priority: must
depends-on: []
estimate: S
owner: unassigned
updated: 2026-08-01
---

# T01 — Spike: prove committed relative links survive git clone-and-go

## Context

The epic's riskiest assumption ([../epic.md](../epic.md), Open questions) is
that a repo with committed skill content in `.agents/skills/<name>` and a
committed *relative* symlink `.claude/skills/<name> →
../../.agents/skills/<name>` yields working skills on a fresh `git clone`
with no gskill run. If this fails anywhere we support, the epic re-frames
before any production code moves. This is a discovery task: its output is
evidence and a decision, not shipped code.

## What to do

- Build a throwaway fixture repo (script or test under `test/e2e`, may be
  temporary): vendored skill dir with `SKILL.md`, relative symlink in
  `.claude/skills/`, both committed.
- Clone it into a fresh temp dir; assert the skill file is readable through
  the agent path. Run on macOS and Linux CI runners.
- Windows is unsupported (epic non-goal, 2026-08-01), so no fallback design:
  just verify that a symlink-less checkout (`git -c core.symlinks=false`)
  is *detectable* (plain file containing link text) and specify the one
  error message `check`/`doctor` should emit for it.
- Record findings and the decision in this file's Notes and in the epic's
  Open questions.

## Acceptance criteria

- [ ] e2e demonstration passes on macOS and Linux: fresh clone → skill
      content readable at `.claude/skills/<name>/SKILL.md` with zero gskill
      commands.
- [ ] The `core.symlinks=false` state is documented with the exact observed
      artifact and the specified `check`/`doctor` error message (no
      fallback/reconciliation designed — Windows is out of scope).
- [ ] Go/no-go note written; epic Open question 1 marked answered.

## Out of scope

- Any change to installer/activation production code (T03).
- Windows in any form — unsupported platform (epic non-goal); no fallback or
  reconciliation design.

## Notes

Current absolute-link creation to be replaced: `internal/active/active.go:122`,
`internal/installer/installer.go:552`. Existing e2e helpers:
`test/e2e/e2e_helpers_test.go`.
