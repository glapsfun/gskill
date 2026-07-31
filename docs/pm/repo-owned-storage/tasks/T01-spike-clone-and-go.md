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
updated: 2026-07-31
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
- Exercise the failure mode: clone with `git -c core.symlinks=false` and
  record what appears (a plain file containing the link text). From this,
  write the copy-fallback + `gskill sync` reconciliation rule: how gskill
  detects a degraded checkout and converts links to copies (and back).
- Record findings and the decision in this file's Notes and in the epic's
  Open questions.

## Acceptance criteria

- [ ] e2e demonstration passes on macOS and Linux: fresh clone → skill
      content readable at `.claude/skills/<name>/SKILL.md` with zero gskill
      commands.
- [ ] The `core.symlinks=false` degraded state is documented with the exact
      observed artifact and the specified reconciliation rule for `sync`.
- [ ] Go/no-go note written; epic Open question 1 marked answered.

## Out of scope

- Any change to installer/activation production code (T03).
- Windows CI wiring — the degraded-checkout simulation covers the semantics.

## Notes

Current absolute-link creation to be replaced: `internal/active/active.go:122`,
`internal/installer/installer.go:552`. Existing e2e helpers:
`test/e2e/e2e_helpers_test.go`.
