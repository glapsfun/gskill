# PManager memo

_Last updated: 2026-07-29_

## 1. Product context

GSKILL is a Go CLI package manager for `SKILL.md`-based AI agent skills. It
resolves, installs, locks, verifies, and restores skill environments across
supported agents. Its public command grammar, generated command reference, and
Diataxis documentation are all maintained in this repository.

## 2. Business goals & north star

| Goal | Metric / north star | Source |
| :--- | :--- | :--- |
| Keep the core CLI workflow small and discoverable | Visible canonical top-level command count | [user, 2026-07-29] |

## 3. Stakeholders

| Who | Cares about | Consulted via |
| :--- | :--- | :--- |
| GSKILL maintainers | Public compatibility, release communication, quality gate | Code review and release review |
| CLI users and automation authors | Migration away from retired commands | Published documentation and release notes |

## 4. Conventions & constraints

- The full definition-of-done gate is `./scripts/verify.sh`.
- Generated command documentation must be regenerated from the CLI grammar.
- Existing dirty worktree changes are user-owned unless explicitly included in
  an approved task; preserve them while implementing adjacent work.
- This epic intentionally specifies immediate breaking removal, not aliases or
  a deprecation cycle.

## 5. Changelog

- 2026-07-29 — spec'd `cli-surface-cleanup`; recorded the requested immediate
  removal of unused CLI paths and preservation of internal auto-init/search.
