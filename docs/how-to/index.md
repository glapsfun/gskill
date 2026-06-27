# How-to guides

Problem-oriented recipes. Each guide states its **starting assumptions**, gives a **copy-paste
example**, and tells you the **expected result** (including the exit code where it matters). New to
GSKILL? Start with the [tutorial](../tutorials/getting-started.md) first.

## Installing skills

- [Install a local skill](install-a-local-skill.md) — add a skill from a folder on disk.
- [Add a skill from Git](add-a-git-skill.md) — add a skill from a Git repository with a version
  constraint.
- [Target specific agents](target-specific-agents.md) — install into Claude Code, Codex, Cursor, and/or
  Gemini CLI, and choose project vs global scope.
- [Copy vs symlink](copy-vs-symlink.md) — choose how installed content lands on disk.

## Reproducing & verifying

- [Reproduce exactly with `--frozen-lockfile`](reproduce-with-frozen-lockfile.md).
- [Work offline](work-offline.md) — restore from a warm cache without network.
- [Verify integrity](verify-integrity.md) — re-hash installed content against the lock.
- [Gate CI on drift](gate-ci-on-drift.md) — fail a build when skills drift or updates are available.

## Lifecycle

- [Update and re-lock](update-and-lock.md).
- [Remove a skill and reclaim space](remove-and-gc.md).
- [Sync and repair](sync-and-repair.md) — reconcile disk with the lock; fix broken installs.
- [Inspect with list, info, and diff](inspect-list-info-diff.md).

## Operations

- [Manage the cache](manage-the-cache.md).
- [Configure GSKILL](configure-gskill.md).
- [Set up shell completion](shell-completion.md).
- [Run doctor](run-doctor.md).
- [Use the TUI dashboard](use-the-tui.md).

## Scripting

- [Script GSKILL with `--json`](script-with-json.md).

---

### Not yet supported

The following commands appear in the long-term design but are **not implemented yet**, so they have no
guide: `search`, `export`, and `import`. They are documented here only so you know they are planned.
