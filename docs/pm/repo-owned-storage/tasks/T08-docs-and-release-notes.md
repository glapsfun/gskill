---
id: T08
epic: repo-owned-storage
milestone: M4
title: Update documentation, command reference, and breaking-change release notes
status: todo
priority: must
depends-on: [T06, T07]
estimate: M
owner: unassigned
updated: 2026-07-31
---

# T08 — Update documentation, command reference, and breaking-change release notes

## Context

The docs currently teach the model this epic deletes — the home-vs-repo
split table in `docs/explanation/project-and-global-state.md` states
"nothing under the home is ever required to reproduce a project" built on
store semantics, and `docs/reference/global-store-layout.md` documents dirs
that will no longer exist. Docs are part of the verify gate (docs examples
are tested: `test/integration/docs_examples_test.go`), so they must land
with, not after, the behavior.

## What to do

- Rewrite/retire: `docs/explanation/global-store.md`,
  `docs/explanation/store-and-cache.md`,
  `docs/explanation/project-and-global-state.md`,
  `docs/explanation/reproducibility-model.md`,
  `docs/reference/global-store-layout.md`,
  `docs/reference/project-registry.md`,
  `docs/reference/configuration.md`, `docs/reference/lockfile-schema.md`.
- New explanation page: the repo-owned model — committed `.agents/skills`,
  relative agent links, home clone cache, clone-and-go, repo-size trade-off,
  degraded-checkout (no-symlink) behavior and how `sync` reconciles it.
- Update tutorials/how-tos (`getting-started`, `add-a-git-skill`,
  `install-a-local-skill`) to show committed skills in `git status`.
- Regenerate `docs/reference/commands.md` from the CLI grammar.
- Draft release notes: breaking changes (removed commands/config keys,
  gitignore inversion, storage move), auto-migration behavior, and the
  manual cleanup hint for the orphaned `~/.gskill/store`.
- Add the clone-and-go e2e as a permanent CI test if T01/T04 left it
  temporary.

## Acceptance criteria

- [ ] No documentation page references `store scope`, `gskill store`,
      `gskill projects`, pins, or quarantine except release notes/changelog.
- [ ] `docs/reference/commands.md` matches the shipped grammar (generated,
      not hand-edited); docs-examples integration tests green.
- [ ] Release notes draft enumerates every removed command and config key
      and the migration story.
- [ ] Clone-and-go e2e runs in CI on the default verify path;
      `./scripts/verify.sh` exits 0.

## Out of scope

- Cutting the release itself (release-guru flow, separate).
- pm-doc updates (tracked via the pmanager tracking flow).
