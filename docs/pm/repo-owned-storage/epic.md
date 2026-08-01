---
id: repo-owned-storage
title: Repo-owned skill storage with home reduced to a clone cache
type: initiative
status: approved
business-goal: A gskill project is shareable via git alone — clone equals working skills
owner: vladtara
created: 2026-07-31
updated: 2026-08-01
---

# Repo-owned skill storage with home reduced to a clone cache

## Problem statement (required)

A gskill-managed project cannot share its installed skills through git.
Installed skills materialize as absolute symlinks into the installing user's
`$HOME/.gskill/store`, and gskill itself gitignores the `.agents/` and
`.gskill/` directories, so the only committed artifact is `skills-lock.json`.
Every teammate and CI job must install gskill and run `gskill install` before
any skill exists, and the links can never be committed because they embed one
user's home path. The machinery that enables this model — a content-addressed
global store with pins, quarantine, GC, verify/repair, a cross-project
registry, and three inconsistent store locations — is a large maintenance
surface serving a linking model the product no longer wants.

## Evidence (required)

| Source | Finding | Kind |
| :--- | :--- | :--- |
| [internal/active/active.go:122] | Active links are created as `filepath.Abs(storePath)` symlinks into the home store | behavioral |
| [internal/installer/installer.go:552] | Agent links (`.claude/skills/<name>`) are absolute symlinks to the active path | behavioral |
| [internal/app/install.go:1070] | gskill appends `.gskill/` and `.agents/` to `.gitignore` — skill content is structurally uncommittable | behavioral |
| [internal/installer/installer.go:344] | Git fetches already land in a commit-keyed cache (`cache.Has(commit)` / `cache.Put`) — the desired home-level role exists today | behavioral |
| [internal/app/project.go:120-149; internal/app/scan.go:55; internal/app/project.go:216-228] | Three distinct store locations coexist (`~/.gskill/store`, `<repo>/.gskill/store`, `config.Dir()/store`) with auto-detect logic | behavioral |
| [internal/home/home.go:65-86] | Home layout carries store, cache, tmp, locks, pins, projects, quarantine — five of seven exist only for the global-store model | behavioral |
| [specs/015-global-skill-store/spec.md] | The home store was a deliberate design buying cross-project dedup, offline restore, GC — this epic knowingly trades dedup away | stated |
| [user] | "user can't share skill or save to git as links to the user current home skill"; wants all state at repo level, home as clone cache only | stated |

No conflicts between behavioral and stated evidence — the code confirms the
user's diagnosis exactly.

**Confidence:** high — the blocking mechanism is directly observed in code;
the one unproven assumption (committed relative symlinks survive git
round-trips and agent tooling) is tested by the first task.

## Hypothesis (required)

We believe that inverting storage ownership — skill content copied into the
repo at `.agents/skills/<name>` and committed, agent directories holding
committed *relative* symlinks,
and `$HOME/.gskill` reduced to a commit-keyed clone cache — will make a fresh
`git clone` yield working skills for teammates and CI with zero gskill
commands, measured by a clone-and-go e2e test passing on the supported
platforms (macOS and Linux) from the first release of this epic.

## Business goal alignment

Sharing is the point of a skills package manager: a team that must install a
second tool before the first tool's output works loses the main adoption
argument. Inverting storage also deletes the largest subsystem in the
codebase (global store + registry + GC + repair), directly serving the
standing goal of a small, maintainable surface [docs/pm/pmanager-memo.md].

## Stakeholders / affected users

| Who | Cares about | Consulted via |
| :--- | :--- | :--- |
| GSKILL maintainers | Breaking-change communication, deleted surface, verify gate | Code review, release review |
| Existing users with home-store projects | Seamless migration, no lost skills | Auto-migration notice, release notes |
| Teams / CI consumers | Clone-and-go behavior, repo size growth | Published documentation |

## Success metrics (required)

| Metric | Role | Current | Target | Window | Measured via |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Fresh clone has working skills with zero gskill commands | primary | 0% (impossible) | 100% on macOS and Linux | first release | clone-and-go e2e test in CI |
| Second install of same skill@commit performs no network fetch | guardrail | pass (store/cache hit) | pass (clone-cache hit) | same | integration test |
| `gskill update` to a newer version remains one command | guardrail | pass | pass | same | integration test |
| `./scripts/verify.sh` | guardrail | green | green throughout | every task | CI |

## Scope

- Install pipeline: clone→home cache, copy skill into `.agents/skills/<name>`,
  create relative agent links; lock and state record repo-relative paths only.
- Stop gitignoring `.agents/`; skill content and agent links are committed.
- Retire the global store machinery: `store/`, `pins/`, `quarantine/`,
  `projects/` home dirs; `gskill store|projects|migrate` commands;
  `storeScope` and registry config keys; reconcile the three store locations
  to zero.
- Home keeps: `cache/` (commit-keyed clones), `locks/`, `tmp/`, `config.toml`.
- Auto-migration: mutating commands detect legacy home-store links and
  rewrite them to repo-owned copies with a one-line notice.
- Documentation and reference updates; breaking-change release notes.

## Non-goals (required)

- **Cross-project content dedup** — accepted loss; the clone cache still
  prevents re-downloads, which is the expensive part.
- **XDG or home-relocation changes** — `~/.gskill` + `GSKILL_HOME` stay as-is
  (spec 015 clarification stands).
- **Windows support** — gskill does not support Windows yet [user,
  2026-08-01]; symlink availability is a platform requirement, and no
  degraded-checkout fallback or reconciliation machinery is designed for it.
  Copy mode survives only as the existing user-chosen `--copy` install mode.
- **Lock schema core-field changes** — the tool-shared fields of
  `skills-lock.json` (spec 012 compatibility) are untouched; only the
  namespaced `gskill` extension may change.
- **Per-project choice of vendored vs restored content** — one model only;
  a config fork was explicitly declined to keep code paths singular.
- **GC of orphaned home stores** — after migration the old store is left for
  the user to delete; we do not destroy data automatically.

## Open questions

- Do committed relative symlinks survive git round-trips on macOS and Linux
  and remain readable by agent tooling?
  → answered by T01 (spike), the first task.
- What exactly shrinks in `.gskill/state.json` once targets are committed?
  → answered by T02 (schema design).

## Related prior work

- [015-global-skill-store](../../../specs/015-global-skill-store/spec.md) —
  built the model this epic inverts; its data-model and migration research
  enumerate every touchpoint.
- [017-implicit-init-gitignore](../../../specs/017-implicit-init-gitignore/) —
  made `.agents/` gitignored; this epic reverses that decision for content.
- [012-skills-lock-compat](../../../specs/012-skills-lock-compat/) — the
  compatibility contract this epic must not break.
- [cli-surface-cleanup](../cli-surface-cleanup/epic.md) — in-flight CLI
  removals; this epic sequences after it to avoid surface collisions.
