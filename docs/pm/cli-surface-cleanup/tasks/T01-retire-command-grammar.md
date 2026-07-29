---
id: T01
epic: cli-surface-cleanup
milestone: M1
title: Retire the command grammar and contract
status: todo
priority: must
depends-on: []
estimate: M
owner: unassigned
updated: 2026-07-29
---

# T01 — Retire the command grammar and contract

## Context

The approved epic removes `status`, `init`, `source`, and `unlink` from the
public CLI. [`internal/cli/root.go`](../../../../internal/cli/root.go) registers
the latter three today, and the worktree already removes the hidden `status`
alias. The CLI grammar, root help, completions, and help-golden inventory form
one public contract, so they must change together before internal cleanup can
begin. See the [epic](../epic.md) and [plan](../plan.md).

## What to do

- Remove the four retired paths from the Kong root grammar and delete the
  command structs that are only CLI adapters (`initCmd`, `sourceCmd`, and
  `unlinkCmd`). Integrate the existing `status`-alias removal without
  overwriting unrelated worktree edits.
- Update root-help and completion inventories, golden pages, alias/suggestion
  expectations, and command-removal tests so the retired words are neither
  advertised nor treated as supported aliases.
- Keep `list` as the tested status and agent-health command. Retain the normal
  `add`, `install`, `remove`, and `search` command grammar unchanged.

## Acceptance criteria

- [ ] Each of `gskill status`, `gskill init`, `gskill source`, and `gskill unlink`
  returns exit code 2 and identifies the first token as an unknown command.
- [ ] `gskill --help`, every supported completion script, and every help-golden
  page contain none of `status`, `init`, `source`, or `unlink` as a top-level
  command.
- [ ] The root command inventory reports exactly 18 visible canonical
  top-level commands, and `gskill list --json` still exposes agent health.
- [ ] Focused CLI help, completion, and list/status replacement tests pass.

## Out of scope

- Deleting application-layer scan or initialization code; that is T02.
- Replacing the removed source-inspection workflows with a different command.
- Updating user-facing guides or generated reference documentation; that is T03.

## Notes

- The baseline root grammar is at `internal/cli/root.go:36-75`.
- Keep `store status` intact; it is distinct from the retired top-level
  `status` alias.
