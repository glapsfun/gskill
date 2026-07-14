# Development workflow

Every change to GSKILL is built test-first and must pass a **single quality gate**.
That gate is enforced identically in three places, so nothing slips through:

| Layer | Mechanism | When it runs |
| --- | --- | --- |
| Developer / AI agent | `scripts/verify.sh` | On demand — the definition of done |
| Git hooks | `pre-commit` framework | Fast checks on commit, full gate on push |
| CI (source of truth) | `.github/workflows/ci.yml` | Every push to `main` and every PR |

All three run the **same shell scripts**, so a green local gate and a green CI run
mean the same thing. The harness deliberately has **no Makefile** — the scripts in
`scripts/` are the batch layer. For the full CI/CD gate catalog (required checks,
action-pinning convention, how to reproduce a gate locally), see
[docs/ci-cd.md](../ci-cd.md).

## Quick start

```bash
# 1. One-time: install pinned dev tools into ./bin and activate git hooks
./scripts/bootstrap.sh

# 2. Run the gate — exit 0 means the work is correct ("definition of done")
./scripts/verify.sh
```

`bootstrap.sh` is reproducible: it installs each tool at the exact version pinned
in `.config/tool-versions` into a project-local `./bin` (gitignored), so your
machine, the AI agent, the git hooks, and CI all run byte-identical tool versions.

## The gate

`scripts/verify.sh` runs these checks in order and stops at the first failure:

1. **`go mod tidy` check** — fails if `go.mod`/`go.sum` are not tidy
2. **format check** — `golangci-lint fmt --diff` (gofmt + gofumpt)
3. **lint** — `go vet` + `golangci-lint run` (40 linters via `.golangci.yml`)
4. **tests** — `go test -race -shuffle=on` with a coverage profile
5. **coverage floor** — total coverage must be ≥ `COVERAGE_MIN` (default `0`)
6. **vulnerabilities** — `govulncheck` on called code paths
7. **secrets** — `gitleaks` scan of the working tree

## Scripts reference

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

## TDD workflow (red → green → refactor)

1. **Red** — write the smallest failing test for the next behavior. Run
   `scripts/test.sh` (or `scripts/tdd.sh` to watch) and see it fail.
2. **Green** — write the minimal code to pass; run the tests again.
3. **Refactor** — clean up with tests green.
4. **Done** — only when `scripts/verify.sh` exits `0`.

Tools live in `./bin` at pinned versions from `.config/tool-versions` — don't rely
on system installs.

## Pinned tooling

Versions live in **`.config/tool-versions`** — the single source of truth read by
both `bootstrap.sh` and CI:

| Tool | What it does |
| --- | --- |
| `golangci-lint` (v2) | Lint + bundled formatters (gofmt, gofumpt) |
| `govulncheck` | Known-vulnerability scanning |
| `gitleaks` | Secret detection |

`shellcheck` is provided by the pre-commit hook, not installed into `./bin`.

## Git hooks (pre-commit framework)

`.pre-commit-config.yaml` wires the framework to the harness scripts (logic lives
in one place, not duplicated in YAML):

- **on commit (fast):** format check, lint, `go test --short`, secret scan, plus
  stock hooks (trailing whitespace, end-of-file, YAML/TOML checks, merge-conflict
  and large-file guards) and `shellcheck` on `scripts/*.sh`.
- **on push:** the full `scripts/verify.sh`.

`bootstrap.sh` installs the hooks automatically (`pre-commit` must be installed).

## Project layout

```text
cmd/gskill/              # CLI entrypoint
internal/                # application, CLI, TUI, and domain packages
scripts/                 # the batch layer: bootstrap, verify, and per-check scripts
docs/                    # user and contributor documentation
.config/tool-versions     # pinned dev-tool versions (single source of truth)
.golangci.yml             # lint + formatter configuration (40 linters)
.gitleaks.toml            # secret-scan config (allowlists non-committed tooling dirs)
.shellcheckrc             # shellcheck configuration for the scripts
.pre-commit-config.yaml   # git-hook wiring → harness scripts
.github/workflows/ci.yml  # CI gate
bin/                      # gitignored; pinned tools land here after bootstrap
```

## Requirements

- **Go 1.26+**
- **[pre-commit](https://pre-commit.com)** (for git hooks)
- A `git` binary

## Building

```bash
go build ./...       # build everything
go run ./cmd/gskill   # print the dev build marker
```

## See also

- [docs/ci-cd.md](../ci-cd.md) — the full CI/CD gate catalog and action-pinning convention.
