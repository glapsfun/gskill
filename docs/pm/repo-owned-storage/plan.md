---
epic: repo-owned-storage
status: active
updated: 2026-08-01
---

# Plan — Repo-owned skill storage with home reduced to a clone cache

## Approach

Prove the riskiest assumption first: a spike (T01) demonstrates that vendored
skill content plus committed relative symlinks actually survive a git
round-trip and remain usable by agents, before any production code moves.
Then invert the pipeline from the inside out — installer activation first
(T03), because every ownership predicate (`active.Owned`,
`guardForeignTarget`, `managedRoots`) keys on store roots and must be
re-keyed to `.agents/skills` before the flows above it can change. The main
alternative — keeping the content-addressed store as hidden cache internals
and copying out of it — was considered and declined: it preserves dedup but
retains the store/GC/verify/repair subsystems this epic exists to delete, and
the clone cache alone already prevents re-downloads. Legacy-surface removal
(T06) comes after the new pipeline is green so the tree is never in a state
where neither model works. Migration and docs close the epic. Everything
lands after the in-flight `cli-surface-cleanup` (021) worktree merges, since
both touch `internal/cli` and the generated command reference.

## Milestones

### M1 — Design validated: clone-and-go proven, schemas decided
Exit criteria:
- [ ] A throwaway e2e demonstrates: repo with committed `.agents/skills/<name>`
      + committed relative `.claude/skills/<name>` link → fresh clone → skill
      readable at the agent path, no gskill run (macOS + Linux).
- [ ] Symlink availability is recorded as a platform requirement
      (macOS/Linux only — Windows unsupported), with the error `check`/
      `doctor` report when a checkout lost its symlinks.
- [ ] New lock-extension and `state.json` field sets are written down,
      including what is removed (`storeHash`, `storeScope`) and what every
      recorded path is relative to.

### M2 — New pipeline live: add/install/update/sync are repo-owned
Exit criteria:
- [ ] `gskill add <git-url> --skill <name>` produces: clone in home cache,
      copy in `.agents/skills/<name>`, relative agent links, repo-relative
      lock/state records — no path under `$HOME` referenced by the repo.
- [ ] `gskill install` (incl. `--frozen-lockfile`) restores from committed
      content when present and verifies it against the lock hash; fetches
      via the clone cache only when content is missing.
- [ ] `gskill update` and `gskill sync` work end-to-end on the new layout.
- [ ] `.gitignore` handling no longer ignores `.agents/`; `.gskill/` stays
      ignored.
- [ ] Second install of same skill@commit in another project hits the clone
      cache (no network) — guardrail test green.

### M3 — Legacy surface retired: home is cache + locks + tmp + config
Exit criteria:
- [ ] `internal/globalstore`, `internal/projreg`, pins/quarantine/projects
      dirs, `gskill store|projects|migrate` commands, `storeScope` and
      registry/privacy config keys are deleted; `home.Ensure` creates only
      `cache/`, `locks/`, `tmp/`.
- [ ] `gskill cache` commands operate on the home clone cache (today they
      point at repo-local `.gskill/cache`).
- [ ] Exactly one store-location concept remains in the codebase (the repo's
      `.agents/skills`); the `config.Dir()/store` and `<repo>/.gskill/store`
      paths are gone.

### M4 — Shipped: legacy projects migrate themselves, docs tell the truth
Exit criteria:
- [ ] A project with home-store absolute links, on its next mutating command,
      ends up fully repo-owned with a one-line notice; its old store objects
      are untouched.
- [ ] Clone-and-go e2e is a permanent CI test.
- [ ] Explanation/reference docs and the command reference are regenerated;
      breaking-change release notes drafted.
- [ ] `./scripts/verify.sh` exits 0 on the final tree.

## Task breakdown & traceability

| Task | Title | Milestone | Priority | Depends on | Status |
| :--- | :--- | :--- | :--- | :--- | :--- |
| T01 | Spike: prove committed relative links clone-and-go | M1 | must | — | todo |
| T02 | Specify repo-owned schemas and path model | M1 | must | T01 | todo |
| T03 | Re-key activation and ownership to `.agents/skills` | M2 | must | T02 | todo |
| T04 | Rework add/install/update/sync onto the repo-owned pipeline | M2 | must | T03 | todo |
| T05 | Invert gitignore handling for `.agents/` | M2 | must | T03 | todo |
| T06 | Retire global store machinery and CLI surface | M3 | must | T04 | todo |
| T07 | Auto-migrate legacy home-store projects | M4 | must | T04, T06 | todo |
| T08 | Update docs, command reference, and release notes | M4 | must | T06, T07 | todo |

## Prioritization

- **Must:** T01–T08. Reasoning: the epic is an inversion, not an addition —
  a half-inverted tree (new pipeline + old store both alive) is strictly
  worse to maintain than either endpoint, so removal (T06), migration (T07)
  and truthful docs (T08) are not optional polish. T01 is must because it is
  the cheapest point to kill the epic if the assumption fails.
- **Should:** none.
- **Could:** none — deliberately; scope discipline is the epic's own goal.
- **Won't (this epic):** cross-project dedup, XDG/home relocation, Windows
  support (and any symlink-fallback machinery for it), core lock-field
  changes, per-project vendored/restored choice, GC of orphaned home stores
  (mirrors epic non-goals).

## Risk register

| Risk | Likelihood | Impact | Mitigation | Trigger / early signal |
| :--- | :--- | :--- | :--- | :--- |
| Committed symlinks break in git round-trips or agent readers on macOS/Linux | low | high | T01 spike first; symlinks declared a platform requirement (Windows out of support) | T01 e2e fails on either platform |
| Ownership predicates re-keying misses a path and `remove`/`sync` deletes user files or fails closed everywhere | med | high | T03 is a dedicated task; adversarial/foreign-target integration tests (`test/integration/adversarial_test.go`) extended before flows change | foreign-target guard failures in CI |
| Vendored content bloats repos with large skills | med | low | Accepted trade-off; document repo-size implications; lock hash detects drift | user reports; none blocking |
| Conflict with in-flight 021 worktree (same CLI files, goldens, command docs) | high | med | Sequence: start after 021 merges; rebase spec docs only | merge conflicts in `internal/cli` |
| Breaking-change fallout for existing users (removed commands, moved cache) | high | med | Auto-migration (T07), explicit release notes (T08), errors with hints for removed commands | issue reports post-release |
| Committed content edited by hand drifts from lock | med | med | `install`/`check` verify committed content against lock `computedHash`; drift reported with repair hint | check/verify test cases |

## Dependencies

| Dependency | Kind | Owner | Status |
| :--- | :--- | :--- | :--- |
| 021 cli-surface-cleanup merged | internal | vladtara | in progress (dirty worktree) |
| `./scripts/verify.sh` gate | internal | repo | available |
| Symlink-capable CI runners (macOS/Linux) | internal | CI | available |

## Definition of done (applies to every task)

- [ ] Acceptance criteria demonstrated
- [ ] Tests written first and passing (project TDD constitution)
- [ ] `./scripts/verify.sh` exits 0
- [ ] Docs updated where behavior changed

## Validation plan

- **Verification:** each milestone's exit criteria are demonstrated by
  integration/e2e tests named in the tasks; M-level checks run in CI on
  every PR of the epic branch.
- **Validation:** after release, the primary metric is validated by the
  permanent clone-and-go e2e in CI and by a manual smoke test — clone a
  real skill-carrying repo on a second machine with no gskill installed and
  confirm the agent sees the skill. Guardrails (no-refetch, one-command
  update) are validated by the retained integration tests. Owner: vladtara,
  at first release containing the epic.

## Changelog

| Date | Change | Why |
| :--- | :--- | :--- |
| 2026-07-31 | Plan created | — |
| 2026-08-01 | Windows dropped from scope: no copy-fallback/degraded-checkout design; symlinks are a platform requirement (macOS/Linux) | User: "we dont suport windows yet" |
