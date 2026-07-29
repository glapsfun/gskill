---
epic: cli-surface-cleanup
status: active
updated: 2026-07-29
---

# Plan — Retire unused GSKILL CLI commands

## Approach

Treat this as a deliberate, immediate breaking CLI reduction rather than a
renaming exercise. First make the command grammar reject the four retired
paths and update its contract tests, including the existing in-progress
`status` removal. Then delete only the CLI and application code that becomes
unreachable: retain `App.Init` for auto-initialization and `App.SourceList` for
search, but remove `source inspect`, `source check`, and `unlink` ownership
paths. Finally regenerate reference documentation and revise examples to lead
users through `add`, `install`, `list`, and `remove`.

The considered alternative is hidden aliases or a deprecation cycle. It is not
chosen because the approved scope is explicit deletion, and aliases preserve
the command, completion, test, and documentation maintenance burden this epic
exists to remove. `source inspect` and `source check` will not be folded into a
new command because their workflows are intentionally out of scope.

## Milestones

### M1 — Retired paths are absent from the CLI contract

Exit criteria:

- [ ] `gskill status`, `gskill init`, `gskill source`, and `gskill unlink`
  each fail as unknown top-level commands with exit code 2.
- [ ] Root help, generated grammar model, command completion, and help golden
  tests contain none of the four retired top-level commands.
- [ ] `gskill list` remains the documented and tested status/agent-health view.

### M2 — Internal dependencies remain intact and obsolete ownership is gone

Exit criteria:

- [ ] Auto-initialization through `add`, `install`, and onboarding still works
  without the public `init` command.
- [ ] `search` still discovers source content through retained internal source
  listing.
- [ ] No production code exports or invokes `App.Unlink`, `SourceInspect`, or
  `SourceCheck` after their CLI paths are removed.

### M3 — Supported documentation and quality gate reflect the smaller surface

Exit criteria:

- [ ] `docs/reference/commands.md` is regenerated and does not list retired
  commands.
- [ ] Supported README, tutorial, and how-to examples use supported workflow
  equivalents and contain no retired-command invocation.
- [ ] `./scripts/verify.sh` exits 0 on the final change.

## Task breakdown & traceability

| Task | Title | Milestone | Priority | Depends on | Status |
| :--- | :--- | :--- | :--- | :--- | :--- |
| T01 | Retire the command grammar and contract | M1 | must | — | todo |
| T02 | Prune unreachable command ownership safely | M2 | must | T01 | todo |
| T03 | Migrate documentation and prove the release surface | M3 | must | T01, T02 | todo |

## Prioritization

- Must: T01 is the user-visible breaking decision and establishes the contract
  every other task relies on. T02 prevents dead application paths from
  surviving behind a removed CLI. T03 is required because the generated
  reference and examples are part of GSKILL's public interface and the quality
  gate is the project definition of done.
- Should: none; every planned task is necessary to ship this narrowly scoped
  removal safely.
- Could: a command-usage instrumentation initiative, but it does not affect
  the approved deletion and would broaden scope.
- Won't (this epic): wider command audit, aliases/deprecation support,
  replacement source-inspection UX, and lock/store redesign.

## Risk register

| Risk | Likelihood | Impact | Mitigation | Trigger / early signal |
| :--- | :--- | :--- | :--- | :--- |
| Removing `source` breaks search internals | med | high | Preserve `App.SourceList`; add a search regression before pruning scan APIs. | Compile failure or search test failure |
| Removing `init` breaks test setup or auto-init | high | med | Keep `App.Init`; replace only CLI invocations with normal add/install setup. | Integration tests invoke `gskill init` |
| Removing `unlink` changes last-agent retention semantics | med | med | Document the intentional migration: exact `install --agent` set or `remove`; delete tests that assert retained-unreferenced state. | Tests rely on `unreferenced: true` |
| Docs drift from generated grammar | med | med | Regenerate the command reference after grammar changes and run its verification path. | `docs/reference/commands.md` lists a retired path |
| Existing user worktree changes are overwritten | low | high | Treat existing status-removal edits as user-owned; integrate around them and stage only intentional files. | Unexpected diff changes outside task scope |

## Dependencies

| Dependency | Kind | Owner | Status |
| :--- | :--- | :--- | :--- |
| Existing uncommitted `status`-removal worktree changes | internal | current worktree owner | available; preserve |
| CLI grammar, golden/help, completion, integration test suites | internal | implementation owner | available |
| `scripts/verify.sh` quality gate | internal | implementation owner | available |
| Release changelog/version decision | external | maintainer | pending implementation |

## Definition of done (applies to every task)

- [ ] Acceptance criteria demonstrated
- [ ] Tests written or updated and passing
- [ ] Code reviewed
- [ ] Documentation updated where behavior changed
- [ ] Existing unrelated worktree changes preserved

## Validation plan

- **Verification:** Run focused command, unit, integration, help-golden,
  completion, documentation-example, and search regressions during the three
  milestones; finish with `./scripts/verify.sh`. Confirm the root help/docs
  inventory is exactly 18 visible canonical top-level commands and retired
  invocations return usage code 2.
- **Validation:** At release review, inspect the published root help and
  documentation navigation for the 18-command target, and record any
  compatibility reports or support requests concerning the removed commands
  during the first release window. The maintainer owns that qualitative
  post-release check because no command-usage telemetry exists today.

## Changelog

| Date | Change | Why |
| :--- | :--- | :--- |
| 2026-07-29 | Plan created | User requested planning only for removal of unused CLI commands. |
