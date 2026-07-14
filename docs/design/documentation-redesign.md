# Documentation redesign

This page records the research, decisions, and rollout plan behind GSKILL's
documentation redesign. It exists so later phases (see the phase table below)
have a committed reference instead of re-deriving scope from scratch.

## 1. Context

GSKILL's documentation had grown organically: a 305-line root README mixing
user-facing install/usage content with contributor-facing TDD-harness internals,
a partial [Diátaxis](https://diataxis.fr/)-organized `docs/` tree (tutorials,
how-to, reference, explanation — but no user guide, examples, or troubleshooting
sections), and — discovered during this work — two live inaccuracies: several
pages still instructed users to commit a `gskill.toml` file that was removed from
the codebase in commit `09085ec`, and the README/`CLAUDE.md` linked to
`docs/tdd-workflow.md`, a path that doesn't exist (the real content lived at the
gitignored `.docs/tdd-workflow.md`, invisible to anyone who only has the cloned
repo). A third inaccuracy — `docs/how-to/index.md` listing `search` as
unimplemented when it has real, working flags — was found and fixed alongside the
other two.

## 2. Current problems

- The root README served two audiences (new users and contributors) in one
  undifferentiated scroll, burying the "how do I install this" content under
  quality-gate internals.
- `gskill.toml` was referenced as a file to commit in 8 files despite being
  removed from the codebase.
- The TDD-workflow link in README and `CLAUDE.md` pointed at a nonexistent path.
- `docs/how-to/index.md` listed a shipped command (`search`) as not implemented.
- The badge row (a single OpenSSF Scorecard badge) didn't communicate CI status,
  release version, Go version, platform support, or license at a glance.
- No `docs/user-guide/`, `docs/examples/`, or `docs/troubleshooting/` sections
  existed.

## 3. Goals

- A README that a new user can read start-to-finish and come away able to
  install GSKILL, add a skill, restore a project, and verify it — without
  reading source code.
- Every command and flag shown anywhere in the documentation verified against
  the live CLI, not assumed from memory or copied from older docs.
- Badges that convey real, verified project health signals with working links.
- A documentation architecture (Diátaxis: tutorials, how-to, reference,
  explanation, plus user-guide, examples, troubleshooting, design) with a clear
  content-migration plan, even where later phases build the missing sections.

## 4. Non-goals

- Redesigning GSKILL command behavior.
- Building `docs/user-guide/`, `docs/examples/`, `docs/troubleshooting/`, the
  9 new/renamed how-to pages, or `scripts/docs-check.sh` in this phase (see the
  phase table).
- A separate documentation website.
- Badges for services with no working data source (e.g. code coverage — CI only
  uploads `coverage.out` as an artifact; no external coverage service is
  configured).

## 5. Target audiences

- **New users** evaluating or adopting GSKILL — served by the README and the
  [getting-started tutorial](../tutorials/getting-started.md).
- **Existing users** solving a specific task — served by
  [how-to guides](../how-to/index.md) and [reference](../README.md#reference).
- **Contributors** changing GSKILL itself — served by
  [docs/contributing/development.md](../contributing/development.md).

## 6. README information hierarchy

Title + tagline → badges → one-paragraph overview → why GSKILL → key features →
supported agents/platforms → quick start → core workflow → common examples →
documentation navigation → project status and compatibility → security and
integrity → contributing → license. Contributor/TDD-harness content that used to
live in the README moved to `docs/contributing/development.md`.

## 7. Badge strategy

| Badge | Source | Why |
| --- | --- | --- |
| CI | GitHub Actions workflow badge, `branch=main` | Live default-branch build status |
| Release | shields.io `github/v/release` | Latest tag, no manual bumping |
| Go | shields.io `github/go-mod/go-version` | Reads `go.mod` live |
| Platforms | shields.io static badge | No dynamic source exists for OS+arch support |
| License | shields.io `github/license` | GitHub-detected license (MIT) |
| OpenSSF Scorecard | scorecard.dev | Already configured via `.github/workflows/scorecard.yml` |
| Go Report Card | goreportcard.com | Works for any public Go module, no setup required |
| Downloads | shields.io `github/downloads` | GitHub releases API, no setup required |

A code-coverage badge was explicitly excluded: CI uploads `coverage.out` as a
build artifact only, with no external coverage service (Codecov, Coveralls)
configured — adding a badge would require standing up a new service, which is
out of scope for a documentation-only phase.

## 8. Documentation architecture

```text
docs/
├── README.md              # landing page (Phase 4 rewrite; gskill.toml fix only this phase)
├── tutorials/              # existing — gskill.toml references fixed this phase
├── how-to/                 # existing, 20 pages — gskill.toml + search-listing fixed this phase
├── reference/               # existing, 6 pages (2 generated) — untouched this phase
├── explanation/              # existing, 4 pages — untouched this phase (already accurate)
├── contributing/              # NEW this phase — development.md
├── design/                     # NEW this phase — this file
├── user-guide/                  # Phase 2 (not built yet)
├── examples/                     # Phase 3 (not built yet)
└── troubleshooting/                # Phase 4 (not built yet)
```

## 9. Content migration map

| Page | Disposition | Phase |
| --- | --- | --- |
| `README.md` | Full rewrite | 1 (this phase) |
| `docs/contributing/development.md` | New — TDD/harness content moved from old README | 1 |
| `docs/design/documentation-redesign.md` | New — this file | 1 |
| `docs/README.md` | `gskill.toml` line fixed only | 1 (full landing-page rewrite: Phase 4) |
| `docs/how-to/remove-and-gc.md`, `reproduce-with-frozen-lockfile.md`, `gate-ci-on-drift.md`, `install-a-local-skill.md`, `inspect-list-info-diff.md`, `update-and-lock.md`, `index.md` | Targeted `gskill.toml`/`search`-listing fixes only | 1 |
| `docs/tutorials/getting-started.md` | `gskill.toml` narrative rewritten | 1 |
| `docs/reference/*`, `docs/explanation/*` | Retained as-is (already accurate) | — |
| `docs/user-guide/index.md`, `concepts.md`, `project-lifecycle.md`, `skill-sources.md`, `agent-targeting.md`, `lockfile-workflow.md`, `installation-modes.md`, `updates.md`, `integrity-and-verification.md`, `offline-and-cache.md`, `tui.md`, `automation-and-ci.md` | New | 2 |
| `docs/examples/single-agent.md`, `multi-agent.md`, `local-skill.md`, `existing-lockfile.md`, `frozen-ci-install.md`, `offline-install.md`, `update-workflow.md` + `examples/` fixtures | New | 3 |
| `docs/troubleshooting/index.md`, `installation.md`, `lockfile-errors.md`, `agent-detection.md`, `integrity-errors.md`, `permissions-and-symlinks.md`, `network-and-authentication.md`, `tui-and-terminal.md` | New | 4 |
| `docs/how-to/install-gskill.md`, `add-a-skill.md`, `install-for-multiple-agents.md`, `restore-from-lockfile.md`, `use-frozen-lockfile.md`, `update-skills.md`, `remove-a-skill.md`, `change-target-agents.md`, `use-gskill-in-ci.md`, `import-an-existing-lockfile.md` | New/renamed (may supersede existing pages with different names, e.g. `target-specific-agents.md`) | 4 |
| `docs/README.md` full landing-page rewrite | Rewrite | 4 |
| `scripts/docs-check.sh` + CI wiring | New | 5 |

## 10. Automation and validation

**This phase:** manual verification only. Every command/flag shown in changed
files was checked against `gskill <cmd> --help` output; every relative link
added or changed was checked with `test -f`; `./scripts/verify.sh` confirms no
code/tooling regression.

**Phase 5 (not built yet):** `scripts/docs-check.sh`, wired into
`scripts/verify.sh` and CI, should check: generated command/exit-code references
are current (`go run ./cmd/gen-reference` diff), README commands parse via a
smoke test, documented flags exist, internal relative links are valid, code
fences are closed, badge URLs return 200, required documentation pages exist,
and obsolete references (like `gskill.toml`) don't reappear.

## 11. Rollout plan

Phase 1 (this phase) ships README + badges + status + design doc + contributing
doc + bug fixes as one PR-sized unit of related commits. Phases 2–5 are each a
separate brainstorm → plan → implementation cycle, scoped when they're picked up,
using the migration map above as the starting reference.

## 12. Risks

- Badge services (Go Report Card, shields.io) changing availability or rate
  limits — low risk; verified working at implementation time; not a dependency
  of `scripts/verify.sh`.
- `docs/contributing/development.md` drifting from `docs/ci-cd.md` over time —
  mitigated by linking to the CI/CD gate catalog rather than duplicating it.
- Scope creep back toward the full original brief in a single phase — mitigated
  by this document's explicit phase table and non-goals.

## 13. Acceptance criteria

1. `README.md` explains what GSKILL is and why it's useful, in the agreed
   section order, and is user-facing only.
2. A new user can install GSKILL and complete `init` → `add` → `install
   --frozen-lockfile` → `verify` end-to-end from the README alone.
3. The badge row renders eight working badges, each linking somewhere
   meaningful.
4. The project-status section states concrete facts, not an unsubstantiated
   "production ready" claim.
5. `gskill.toml` is not referenced as a file to commit anywhere in the repo's
   tracked documentation.
6. `docs/tdd-workflow.md` is no longer linked from README or `CLAUDE.md`.
7. This file exists with all 13 required sections and a complete migration map.
8. `docs/contributing/development.md` exists with the relocated TDD/harness
   content and a link to `docs/ci-cd.md`.
9. Every command/flag shown anywhere in the changed files was verified against
   the current CLI's `--help` output.
10. `./scripts/verify.sh` exits `0`.

## See also

- [Reproducibility model](../explanation/reproducibility-model.md)
- [Integrity and trust](../explanation/integrity-and-trust.md)
- [Development workflow](../contributing/development.md)
