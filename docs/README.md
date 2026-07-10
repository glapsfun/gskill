# GSKILL Documentation

**GSKILL** is a reproducible package manager for agentic AI skills. It installs, versions, locks,
verifies, and restores `SKILL.md` instruction bundles across AI coding agents, developer machines, and
CI. Commit `gskill.toml` (intent) and `skills-lock.json` (resolved reality), and reproduce a byte-identical
skill environment anywhere with `gskill install --frozen-lockfile`.

This documentation follows the [Diátaxis](https://diataxis.fr/) framework — four kinds of docs, each
with one job.

## 📚 Start here

| If you want to… | Go to |
| --- | --- |
| **Learn** GSKILL from scratch, step by step | [Tutorial: Getting started](tutorials/getting-started.md) |
| **Do** a specific task (recipes with examples) | [How-to guides](how-to/index.md) |
| **Look up** a command, flag, exit code, or file format | [Reference](#reference) |
| **Understand** how and why GSKILL works | [Explanation](#explanation) |

## Tutorial

A single guided lesson from an empty directory to a committed, reproducible skill environment.

- [Getting started](tutorials/getting-started.md)

## How-to guides

Problem-oriented recipes — one per feature. See the [full index](how-to/index.md). Highlights:

- [Install a local skill](how-to/install-a-local-skill.md)
- [Add a skill from Git](how-to/add-a-git-skill.md)
- [Reproduce exactly with `--frozen-lockfile`](how-to/reproduce-with-frozen-lockfile.md)
- [Verify integrity](how-to/verify-integrity.md)
- [Gate CI on drift](how-to/gate-ci-on-drift.md)
- [Script GSKILL with `--json`](how-to/script-with-json.md)

## Reference

Authoritative, exhaustive lookup.

- [Commands](reference/commands.md) — *generated from the CLI*
- [Exit codes](reference/exit-codes.md) — *generated from the CLI*
- [Configuration](reference/configuration.md)
- [`gskill.toml` manifest schema](reference/manifest-schema.md)
- [`skills-lock.json` lockfile schema](reference/lockfile-schema.md)
- [`SKILL.md` frontmatter schema](reference/frontmatter-schema.md)
- [Supported agents](reference/agents.md)

## Explanation

Understanding-oriented discussion.

- [The reproducibility model](explanation/reproducibility-model.md)
- [Integrity and trust](explanation/integrity-and-trust.md)
- [The store and the cache](explanation/store-and-cache.md)
- [Multi-agent installs](explanation/multi-agent-installs.md)

---

> The two reference pages marked *generated* are produced by `go run ./cmd/gen-reference` and must not
> be hand-edited — they are kept in lockstep with the tool by a golden test in CI.
