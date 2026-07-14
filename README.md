# GSKILL
Reproducible package management for AI agent skills.

[![CI](https://github.com/glapsfun/gskill/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/glapsfun/gskill/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/glapsfun/gskill)](https://github.com/glapsfun/gskill/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/glapsfun/gskill)](go.mod)
[![Platforms](https://img.shields.io/badge/platforms-linux%20%7C%20macos%20%7C%20amd64%20%7C%20arm64-blue)](#project-status-and-compatibility)
[![License](https://img.shields.io/github/license/glapsfun/gskill)](LICENSE)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/glapsfun/gskill/badge)](https://scorecard.dev/viewer/?uri=github.com/glapsfun/gskill)
[![Go Report Card](https://goreportcard.com/badge/github.com/glapsfun/gskill)](https://goreportcard.com/report/github.com/glapsfun/gskill)
[![Downloads](https://img.shields.io/github/downloads/glapsfun/gskill/total)](https://github.com/glapsfun/gskill/releases)

GSKILL installs, versions, locks, verifies, and restores `SKILL.md`-based AI agent
skills across Claude Code, Codex, Cursor, Gemini CLI, developer machines, and CI.
Commit `skills-lock.json`; teammates and CI reproduce a byte-identical skill
environment anywhere with `gskill install --frozen-lockfile`.

## Why GSKILL

AI agent skills — `SKILL.md` instruction bundles — are usually installed by hand:
cloned into a dotfile directory, copied between projects, upgraded whenever someone
remembers. That leaves no record of what's installed, no way to verify it wasn't
tampered with, and no way to reproduce it on a second machine or in CI. GSKILL
treats skills like dependencies: resolved, content-hashed, locked, and restorable
byte-for-byte from a single committed file.

## Key features

- **Reproducible installs** — `skills-lock.json` records intent and resolved
  reality together; `--frozen-lockfile` restores exactly, or fails closed.
- **Verified integrity** — every install is checked against a recorded content
  hash; `gskill verify` re-checks on demand; skill content is never executed.
- **Multi-agent, one store** — a skill is resolved and stored once and shared by
  every agent that targets it, via symlinks (or copies where unsupported).
- **Scriptable** — every capability is reachable from the CLI, with `--json`
  output and documented exit codes for CI.
- **Signed releases** — checksummed, cosign-signed archives with SBOMs and
  build-provenance attestations.

## Supported agents and platforms

| Agent | Agent ID | Marker |
| --- | --- | --- |
| Claude Code | `claude` | `.claude/` |
| Codex | `codex` | `.codex/` |
| Cursor | `cursor` | `.cursor/` |
| Gemini CLI | `gemini-cli` | `.gemini/` |

gskill ships for **Linux and macOS** on **amd64** and **arm64**. Windows is not
supported. See [Supported agents](docs/reference/agents.md) for detection and
scope details.

## Quick start

**Install** (pick one):

```bash
# Install script — downloads the right archive and verifies its checksum
curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh

# Homebrew (macOS and Linux)
brew install glapsfun/tap/gskill

# Go (requires a Go toolchain)
go install github.com/glapsfun/gskill/cmd/gskill@latest
```

Verify the install with `gskill version`.

**Use it:**

```bash
gskill init                                                      # prepare .gskill/ + .agents/skills/
gskill add github.com/owner/repo --skill example --agent claude  # resolve, install, lock
gskill install --frozen-lockfile                                 # reproduce exactly elsewhere
gskill verify                                                    # re-hash installed content vs the lock
```

Commit `skills-lock.json` — it's the only project state file GSKILL writes; it
records both what you asked for and what was resolved, so anyone can reproduce it
with `gskill install --frozen-lockfile`.

## Core workflow

GSKILL's lifecycle is discover → add → lock → install → verify → update.

### 1. Discover

Search for available skills:

```bash
gskill search kubernetes
```

### 2. Add

Resolve, install, and record a skill:

```bash
gskill add github.com/example/skills --skill kubernetes --agent claude
```

### 3. Lock

Every `add`/`install`/`update` writes the resolution to `skills-lock.json`
automatically — there's no separate lock step to remember.

### 4. Install

Reproduce a locked environment elsewhere, exactly:

```bash
gskill install --frozen-lockfile
```

### 5. Verify

Re-hash installed content against the lock:

```bash
gskill verify
```

### 6. Update

Advance within your version constraints and re-lock:

```bash
gskill update
```

## Common examples

**Install a skill for one agent:**

```bash
gskill add github.com/example/skills --skill security-review --agent claude
```

**Install for multiple agents:**

```bash
gskill add github.com/example/skills --skill security-review --agent claude,codex
```

**Restore a cloned project:**

```bash
gskill install --frozen-lockfile
```

**Preview changes:**

```bash
gskill install --dry-run
```

**Check project health:**

```bash
gskill status
gskill verify
```

**Update skills:**

```bash
gskill outdated
gskill update
```

## Documentation

Full documentation lives in [`docs/`](docs/README.md), organized by the
[Diátaxis](https://diataxis.fr/) framework:

- **[Tutorial: Getting started](docs/tutorials/getting-started.md)** — from an
  empty directory to a reproducible skill environment.
- **[How-to guides](docs/how-to/index.md)** — task-focused recipes.
- **[Reference](docs/README.md#reference)** — commands, exit codes, configuration,
  and the lockfile/frontmatter schemas.
- **[Explanation](docs/README.md#explanation)** — the reproducibility model,
  integrity and trust, the store, and multi-agent installs.
- **[Contributing](docs/contributing/development.md)** — the TDD workflow and
  quality gate for anyone changing GSKILL itself.

## Project status and compatibility

GSKILL is under active development. The current release is `v0.2.0` (pre-1.0). It
supports Linux and macOS on amd64 and arm64; Windows is not currently supported. It
targets Claude Code, Codex, Cursor, and Gemini CLI. The lockfile format
(`skills-lock.json`) is versioned; breaking CLI or schema changes may occur before
v1.0 and are documented in release notes.

`init`, `add`, `onboard`, `install` (incl. `--frozen-lockfile`/`--offline`),
`verify`, `check`, `outdated`, `update`, `remove`, `sync`, `repair`, `list`,
`info`, `search`, `diff`, `doctor`, `cache`, `config`, `completion`, and `dashboard`
(`tui`) all work today against Git and local sources.

## Security and integrity

Every install is verified against a recorded content hash before it's written; a
mismatch aborts rather than partially installing. Skill content is data, never
executed. Release artifacts are checksummed, cosign-signed, and shipped with SBOMs
and build-provenance attestations — see
[Cut and verify a release](docs/how-to/releasing.md) to verify them yourself. Read
[Integrity and trust](docs/explanation/integrity-and-trust.md) for the full model.

## Contributing

GSKILL is developed test-first behind a single quality gate
(`./scripts/verify.sh`), enforced identically for local development, git hooks,
and CI. See [docs/contributing/development.md](docs/contributing/development.md)
for the full workflow, one-time setup, and script reference.

## License

[MIT](LICENSE)
