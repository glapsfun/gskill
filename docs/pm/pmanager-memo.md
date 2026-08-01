# PManager memo

_Last updated: 2026-08-01_

## 1. Product context

GSKILL is a Go CLI package manager for `SKILL.md`-based AI agent skills. It
resolves, installs, locks, verifies, and restores skill environments across
supported agents. Its public command grammar, generated command reference, and
Diataxis documentation are all maintained in this repository.

## 2. Business goals & north star

| Goal | Metric / north star | Source |
| :--- | :--- | :--- |
| Keep the core CLI workflow small and discoverable | Visible canonical top-level command count | [user, 2026-07-29] |
| A gskill project is shareable via git alone | Fresh clone has working skills with zero gskill commands | [user, 2026-07-31] |

## 3. Stakeholders

| Who | Cares about | Consulted via |
| :--- | :--- | :--- |
| GSKILL maintainers | Public compatibility, release communication, quality gate | Code review and release review |
| CLI users and automation authors | Migration away from retired commands | Published documentation and release notes |

## 4. Conventions & constraints

- The full definition-of-done gate is `./scripts/verify.sh`.
- Storage direction (2026-07-31 decision): repo owns skill content
  (committed `.agents/skills` + relative agent links); `$HOME/.gskill` is a
  clone cache only; global store / dedup was explicitly traded away.
- Supported platforms are macOS and Linux only — Windows is not supported
  yet [user, 2026-08-01]; do not design Windows fallbacks into specs.
- Generated command documentation must be regenerated from the CLI grammar.
- Existing dirty worktree changes are user-owned unless explicitly included in
  an approved task; preserve them while implementing adjacent work.
- This epic intentionally specifies immediate breaking removal, not aliases or
  a deprecation cycle.

## 5. Changelog

- 2026-07-29 — spec'd `cli-surface-cleanup`; recorded the requested immediate
  removal of unused CLI paths and preservation of internal auto-init/search.
- 2026-07-31 — spec'd `repo-owned-storage`; user chose vendored committed
  skill content, committed relative agent links, full removal of the spec-015
  global store, and auto-migration; sequences after cli-surface-cleanup.
- 2026-08-01 — descoped Windows from `repo-owned-storage` (no copy-fallback
  design); learned Windows is not a supported platform product-wide.
