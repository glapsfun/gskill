---
id: cli-surface-cleanup
title: Retire unused GSKILL CLI commands
type: tech-debt
status: approved
business-goal: Keep the primary skill-management workflow small and discoverable.
owner: unassigned
created: 2026-07-29
updated: 2026-07-29
---

# Retire unused GSKILL CLI commands

## Problem statement

GSKILL's visible command surface retains command paths that either duplicate the
normal lifecycle or serve narrowly specialized flows. `init` is unnecessary in
the normal install path, while `source` and `unlink` add top-level paths beyond
the core discover, add, install, inspect, and remove workflow. This makes the
root help and documentation harder to scan, and keeps tests and maintenance
code for behaviors the product owner has requested be deleted.

## Evidence

| Source | Finding | Kind |
| :--- | :--- | :--- |
| [user] | Requested a review and deletion of unused CLI commands, specifically naming `init`, `source`, and `unlink`; asked for planning only. | stated |
| [internal/cli/init.go:16-24] | `init` documents that `add` and `install` already initialize a project; its unique public option is `--lock`. | behavioral |
| [internal/cli/add.go:52-59] | `add` advertises that no separate setup step is required and offers `add <source> --list`. | behavioral |
| [internal/app/install.go:156-158] | `add --list` returns discovery results before installation. | behavioral |
| [internal/cli/install.go:28-34] | `install --agent` replaces the exact recorded agent set, providing the supported path for narrowing agents. | behavioral |
| [internal/app/find.go:45] | `search` still depends on `App.SourceList`; removing the CLI must not remove that internal capability. | behavioral |
| [internal/cli/root.go:36-75] | The root grammar currently exposes 21 visible canonical top-level commands, including all three candidates. | behavioral |
| [working tree, 2026-07-29] | An uncommitted change already removes the hidden `status` alias in favor of `list`. | behavioral |
| [GitHub issue search, 2026-07-29] | No matching issue establishes actual end-user usage; the decision to call commands unused is product direction, not telemetry. | behavioral |

**Confidence:** medium — code confirms the overlap and removal blast radius, but
there is no command-usage telemetry. Production usage data would raise
confidence about the compatibility cost.

## Hypothesis

We believe removing `status`, `init`, `source`, and `unlink` as immediate
breaking CLI paths will make the root command surface easier to discover for
GSKILL users, measured by reducing visible canonical top-level commands from
21 to 18 in the next released version, while the full verification gate remains
green.

## Business goal alignment

GSKILL's value is a reproducible, understandable skill lifecycle. Fewer
competing entry points reduce the amount users must learn before adding,
inspecting, restoring, or removing skills. Retaining redundant commands adds
documentation, completion, and compatibility burden without serving the chosen
product direction.

## Stakeholders / affected users

- GSKILL CLI users and automation authors, who will need to stop invoking the
  removed commands.
- GSKILL maintainers, who own the command grammar, documentation, releases,
  and compatibility decision.
- Package/release consumers, who need the breaking change called out in release
  notes when implementation is shipped.

## Success metrics

| Metric | Role | Current | Target | Window | Measured via |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Visible canonical top-level commands | primary | 21 | 18 | At merge and next release | `gskill --help` / `DocsModel` command inventory |
| Full quality gate passes | guardrail | Required | Exit 0 | Before merge | `./scripts/verify.sh` |
| Retired-command references in supported docs/completions | guardrail | Present | 0 | Before merge | targeted `rg` and generated command reference |

## Scope

- Remove the immediate breaking CLI entry points `init`, `source` (all three
  subcommands), and `unlink`.
- Complete the already-started removal of the hidden `status` alias; `list`
  remains the supported status view.
- Preserve the internal initialization and source-listing capabilities used by
  install/onboarding and search.
- Remove only application code made unreachable by these CLI removals.
- Update generated command reference, help goldens, completion, user guides,
  examples, tests, and release-facing migration notes.

## Non-goals

- A wider audit or removal of other top-level commands; no evidence-backed
  decision has been made for them in this epic.
- Compatibility aliases, deprecation warnings, or automatic command rewrites;
  this is an immediate breaking cleanup.
- A replacement for `source inspect` or `source check`; those niche workflows
  are intentionally retired rather than redesigned.
- Changes to lockfile schema, stores, discovery semantics, or agent adapters.
- Implementing the plan in this planning-only run.

## Open questions

None for planning. Before implementation, maintainers should confirm the
release version and changelog location that communicate the breaking change.

## Related prior work

None. `docs/pm/` did not exist when this epic was created.
