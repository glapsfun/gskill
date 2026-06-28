# gskill

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/glapsfun/gskill/badge)](https://scorecard.dev/viewer/?uri=github.com/glapsfun/gskill)

**GSKILL** is a reproducible package manager for agentic AI skills — it installs,
versions, locks, verifies, and restores SKILL.md-based skill environments across
AI coding agents, developer machines, and CI. Commit `gskill.toml` and
`gskill.lock`, and reproduce a byte-identical skill environment anywhere.

> **Status:** the v1 engine is implemented test-first behind the quality harness
> below. `init`, `add`, `install` (incl. `--frozen-lockfile`/`--offline`),
> `verify`, `check`, `outdated`, `update`, `lock`, `remove`, `sync`, `repair`,
> `list`, `info`, `diff`, `doctor`, `cache`, `config`, `completion`, and `tui`
> all work against Git and local sources for Claude Code, Codex, Cursor, and
> Gemini CLI.

---

## Installation

gskill ships for **Linux and macOS** on **amd64 (x86_64)** and **arm64**. Windows is not
supported.

**Install script** (downloads the right archive and verifies its checksum):

```bash
curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh
```

Override the version or install directory with env vars:
`VERSION=v0.4.0 INSTALL_DIR="$HOME/.local/bin" sh -c "$(curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh)"`.

**Homebrew** (macOS and Linux):

```bash
brew install glapsfun/tap/gskill
```

**Go** (requires a Go toolchain):

```bash
go install github.com/glapsfun/gskill/cmd/gskill@latest
```

Verify the install with `gskill version`. Release artifacts are checksummed and
cosign-signed with SBOMs and build-provenance attestations; see
[docs/how-to/releasing.md](docs/how-to/releasing.md) to verify them.

---

## Usage

```bash
gskill init                               # scaffold gskill.toml + .gskill/
gskill add github.com/owner/repo/skill    # resolve, install, lock
gskill add ./local/skill --agent codex    # install a local skill into one agent
gskill add github.com/openai/skills --skill code-review   # pick one of many
gskill add github.com/openai/skills --skill '*'           # install all valid skills
gskill add github.com/openai/skills --list                # list skills, install nothing
gskill source list github.com/openai/skills   # enumerate skills in a source
gskill source check github.com/openai/skills  # report invalid/duplicate skills (exit 3)
gskill find kubernetes --owner glapsfun        # search a GitHub owner's repos
gskill install --frozen-lockfile          # reproduce exactly from gskill.lock
gskill verify                             # re-hash installed content vs the lock
gskill check --fail-on-drift              # fast CI drift gate
gskill outdated --exit-code               # exit 8 if updates are available
gskill update [skill...]                  # advance within constraints, rewrite lock
gskill remove <skill>                     # uninstall + GC the store
gskill list --json                        # machine-readable inventory
```

Commit `gskill.toml` (intent) and `gskill.lock` (resolved reality); reproduce a
byte-identical environment anywhere with `gskill install --frozen-lockfile`.

### Command reference

| Command | Purpose |
| --- | --- |
| `init` | Scaffold the manifest, `.gskill/` state dir, and gitignore hints. |
| `add <source>` | Resolve, install, and record a new skill. |
| `install` | Materialize all declared skills (additive, idempotent). |
| `remove <name>` | Uninstall; drop from manifest + lock; GC the store. |
| `update [name]` | Advance resolutions within constraints; rewrite the lock. |
| `lock` | Recompute the lock from the manifest without bumping pins. |
| `sync` | Make disk match the lock (`--prune` removes orphans). |
| `repair` | Re-materialize broken installs; clean orphaned staging. |
| `verify` | Re-hash installed content against the lock (fail-closed). |
| `check` | Fast metadata drift report (`--fail-on-drift`). |
| `outdated` | Show available updates (`--exit-code` → 8). |
| `list` / `info` / `diff` | Inspect installed skills. |
| `doctor` | Check git, detected agents, and declared requirements. |
| `cache` / `config` / `completion` | Cache, configuration, and shell completion. |
| `tui` | Interactive dashboard with a sanitized SKILL.md preview. |

### Exit codes

| Code | Meaning | Code | Meaning |
| --- | --- | --- | --- |
| 0 | success | 7 | drift detected (`--fail-on-drift`) |
| 1 | generic error | 8 | update available (`outdated --exit-code`) |
| 2 | usage error | 9 | unsupported / undetected agent |
| 3 | invalid manifest | 10 | partial installation |
| 4 | lockfile mismatch (`--frozen-lockfile`) | 11 | authentication failure |
| 5 | source unavailable / network | 12 | cache / lock failure |
| 6 | integrity failure (checksum) | | |

---

## Documentation

Full documentation lives in [`docs/`](docs/README.md), organized by the
[Diátaxis](https://diataxis.fr/) framework:

- **[Tutorial](docs/tutorials/getting-started.md)** — learn GSKILL from an empty
  directory to a reproducible skill environment.
- **[How-to guides](docs/how-to/index.md)** — task-focused recipes with examples,
  one per feature.
- **[Reference](docs/README.md#reference)** — commands, flags, exit codes, and the
  manifest/lockfile/frontmatter schemas. The command and exit-code references are
  generated from the CLI (`go run ./cmd/gen-reference`) and kept in lockstep by a
  golden test.
- **[Explanation](docs/README.md#explanation)** — the reproducibility model,
  integrity & trust, the store vs the cache, and multi-agent installs.

---

## TDD & Quality Harness

Every change to GSKILL is built test-first and must pass a **single quality gate**.
That gate is enforced identically in three places, so nothing slips through:

| Layer | Mechanism | When it runs |
| --- | --- | --- |
| Developer / AI agent | `scripts/verify.sh` | On demand — the definition of done |
| Git hooks | `pre-commit` framework | Fast checks on commit, full gate on push |
| CI (source of truth) | `.github/workflows/ci.yml` | Every push to `main` and every PR |

All three run the **same shell scripts**, so a green local gate and a green CI run
mean the same thing. The harness deliberately has **no Makefile** — the scripts in
`scripts/` are the batch layer.

### Quick start

```bash
# 1. One-time: install pinned dev tools into ./bin and activate git hooks
./scripts/bootstrap.sh

# 2. Run the gate — exit 0 means the work is correct ("definition of done")
./scripts/verify.sh
```

`bootstrap.sh` is reproducible: it installs each tool at the exact version pinned
in `.config/tool-versions` into a project-local `./bin` (gitignored), so your
machine, the AI agent, the git hooks, and CI all run byte-identical tool versions.

### The gate

`scripts/verify.sh` runs these checks in order and stops at the first failure:

1. **`go mod tidy` check** — fails if `go.mod`/`go.sum` are not tidy
2. **format check** — `golangci-lint fmt --diff` (gofmt + gofumpt)
3. **lint** — `go vet` + `golangci-lint run` (40 linters via `.golangci.yml`)
4. **tests** — `go test -race -shuffle=on` with a coverage profile
5. **coverage floor** — total coverage must be ≥ `COVERAGE_MIN` (default `0`)
6. **vulnerabilities** — `govulncheck` on called code paths
7. **secrets** — `gitleaks` scan of the working tree

### Scripts reference

Each script is independently runnable, sources `scripts/lib.sh`, and resolves
tools from `./bin` first.

| Script | Purpose | Notable flags |
| --- | --- | --- |
| `scripts/bootstrap.sh` | Install pinned tools into `./bin`; install git hooks | — |
| `scripts/verify.sh` | The full gate / definition of done | — |
| `scripts/tdd.sh` | Watch `*.go` and re-run tests on change (inner TDD loop) | — |
| `scripts/fmt.sh` | Format Go code | `--check` (no writes; for CI/hooks) |
| `scripts/lint.sh` | `go vet` + `golangci-lint` | passes args through (e.g. `--fix`) |
| `scripts/test.sh` | Race + coverage tests → `coverage.out` | `--short` (fast, no race/coverage) |
| `scripts/cover.sh` | Enforce `COVERAGE_MIN` against the profile | reads `COVERAGE_MIN` env |
| `scripts/vuln.sh` | `govulncheck` | — |
| `scripts/secrets.sh` | `gitleaks` secret scan | — |

### TDD workflow (red → green → refactor)

1. **Red** — write the smallest failing test for the next behavior. Run
   `scripts/test.sh` (or `scripts/tdd.sh` to watch) and see it fail.
2. **Green** — write the minimal code to pass; run the tests again.
3. **Refactor** — clean up with tests green.
4. **Done** — only when `scripts/verify.sh` exits `0`.

More detail (including guidance for AI agents) is in
[`docs/tdd-workflow.md`](docs/tdd-workflow.md).

### Pinned tooling

Versions live in **`.config/tool-versions`** — the single source of truth read by
both `bootstrap.sh` and CI:

| Tool | What it does |
| --- | --- |
| `golangci-lint` (v2) | Lint + bundled formatters (gofmt, gofumpt) |
| `govulncheck` | Known-vulnerability scanning |
| `gitleaks` | Secret detection |

`shellcheck` is provided by the pre-commit hook, not installed into `./bin`.

### Git hooks (pre-commit framework)

`.pre-commit-config.yaml` wires the framework to the harness scripts (logic lives
in one place, not duplicated in YAML):

- **on commit (fast):** format check, lint, `go test --short`, secret scan, plus
  stock hooks (trailing whitespace, end-of-file, YAML/TOML checks, merge-conflict
  and large-file guards) and `shellcheck` on `scripts/*.sh`.
- **on push:** the full `scripts/verify.sh`.

`bootstrap.sh` installs the hooks automatically (`pre-commit` must be installed).

### Continuous integration

`.github/workflows/ci.yml` checks out the repo, sets up Go 1.26 with module
caching, runs `scripts/bootstrap.sh`, then runs `scripts/verify.sh` — the same
gate you run locally — and uploads the coverage profile as an artifact.
`.github/dependabot.yml` keeps Go modules and GitHub Actions up to date weekly.

---

## Project layout

```
cmd/gskill/             # CLI entrypoint (prints version for now)
internal/version/       # version package + proof test (the first red→green unit)
scripts/                # the batch layer: bootstrap, verify, and per-check scripts
docs/tdd-workflow.md    # red-green-refactor + definition-of-done
.config/tool-versions   # pinned dev-tool versions (single source of truth)
.golangci.yml           # lint + formatter configuration (40 linters)
.gitleaks.toml          # secret-scan config (allowlists non-committed tooling dirs)
.shellcheckrc           # shellcheck configuration for the scripts
.pre-commit-config.yaml # git-hook wiring → harness scripts
.github/workflows/ci.yml# CI gate
bin/                    # gitignored; pinned tools land here after bootstrap
```

## Requirements

- **Go 1.26+**
- **[pre-commit](https://pre-commit.com)** (for git hooks)
- A `git` binary

## Building

```bash
go build ./...     # build everything
go run ./cmd/gskill # prints: gskill dev
```
