# CI/CD Pipeline Guide

How GSKILL's continuous-integration and release pipeline is hardened, what each gate
enforces, how to reproduce a gate locally, and how to waive a finding. This is the
companion to `specs/004-cicd-best-practices/`.

The golden rule: **`scripts/verify.sh` is the single definition of done.** Every other
check below is *additive* — it hardens or accelerates the pipeline but never replaces,
weakens, or lets you bypass that gate.

---

## Gate catalog

| Gate (status-check name) | Source | Trigger | Disposition |
|--------------------------|--------|---------|-------------|
| `verify` | `scripts/verify.sh` | PR + push | **required** — definition of done |
| `build-test (ubuntu/macos)` | `go build` + `go test -race` | PR + push | **required** |
| `release-dryrun` | `goreleaser check` + snapshot | PR + push | **required** |
| `workflow-audit` | `zizmor` + `actionlint` | PR + push | **required** |
| `codeql` | CodeQL (Go, security-extended) | PR + push + weekly | **required** (high-severity blocks) |
| `pr-title` | semantic-pull-request | PR | **required** |
| `scorecard` | OpenSSF Scorecard | push-to-`main` + weekly | **informational** (grade/trend, not a merge gate) |

Required gates **fail closed**: a degraded, errored, or unavailable required check blocks
merge rather than passing silently. `scorecard` is explicitly *not* a required check.

---

## Action-pinning convention

Every `uses:` reference in every workflow MUST be pinned to a full 40-hex commit SHA with a
trailing version comment:

```yaml
uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
```

Rules:

- **SHA, never a tag or branch.** Mutable tags can be re-pointed at malicious commits; an
  immutable SHA cannot. This is the single highest-leverage supply-chain control.
- **Version at the *end* of the comment** (`# v7.0.0`, not `# v7.0.0 (checkout)`). Dependabot
  rewrites the comment on update only when the version sits at the end of the line.
- **Local actions** (`uses: ./...`) and same-repo reusable workflows are exempt from SHA
  pinning (they are version-controlled here) but are still audited.

Enforced by the `workflow-audit` gate (see below). To pin a new action, resolve its tag to a
SHA:

```sh
gh api repos/<owner>/<repo>/commits/<tag> --jq .sha
```

---

## `workflow-audit` gate (zizmor + actionlint)

Runs `zizmor` (GitHub Actions static analyzer) and `actionlint` (workflow correctness) over
`.github/workflows/`. zizmor's `unpinned-uses` audit requires hash-pinning on **all**
actions; any unpinned/mutable reference fails the gate with a diagnostic naming the
offending `uses:`. zizmor also covers template injection, excessive permissions, cache
poisoning, and dangerous `pull_request_target` use.

Reproduce locally (identical invocation to CI):

```sh
./scripts/bootstrap.sh        # installs actionlint into ./bin; checks for uv
./scripts/audit-workflows.sh  # actionlint + uvx zizmor==$ZIZMOR over .github/workflows
```

Policy lives in `.github/zizmor.yml`.

### Waiving a zizmor finding

Never disable the gate. Waive narrowly, with justification, either:

- inline: `# zizmor: ignore[<rule-id>] — <reason>` on the offending line, or
- in `.github/zizmor.yml` with a comment explaining why.

---

## CodeQL (static application security testing)

`.github/workflows/codeql.yml` runs CodeQL over the Go source (build-mode `autobuild`,
`security-extended` query suite) on every PR, on push to `main`, and weekly. Go cannot use
the build-mode `none` / default-setup path, so this is committed **advanced setup** — which
also lets us SHA-pin the action, scope its token to `security-events: write`, and review it
like any other workflow.

- **Blocking**: with `codeql` required in branch protection, an unresolved **high-severity**
  alert blocks merge. A degraded/errored analysis is a failing required check (fail closed).
- **Retention/trend**: results persist in the repository code-scanning store; the weekly run
  keeps coverage of already-merged code so newly disclosed query findings still surface.

### Waiving a CodeQL finding

A confirmed false positive is **dismissed in the code-scanning UI with a recorded reason**,
or suppressed narrowly in code with a justification comment — never by removing or disabling
the workflow.

---

## OpenSSF Scorecard (supply-chain posture)

`.github/workflows/scorecard.yml` runs on push to `main` and weekly, uploads SARIF to code
scanning, and publishes the aggregate grade (badge in `README.md`). It needs
`security-events: write` (upload SARIF) and `id-token: write` (publish); it runs only on
default-branch/schedule contexts, so those scopes never see untrusted forked-PR code.

**Agreed regression threshold**: the aggregate Scorecard grade should stay **≥ 7.0/10**. A
published grade below 7.0, or a drop of **≥ 1.0** from the previous run, is treated as a
posture regression to be triaged within one scheduled cycle. Scorecard publishes the grade
and per-check SARIF but does **not** natively alert on a drop — maintainers review the badge
and the weekly run; configure a notification on the `scorecard` code-scanning category if
automated alerting is desired. The highest-impact checks here are Pinned-Dependencies,
Token-Permissions, SAST, Branch-Protection, Dependency-Update-Tool, and Signed-Releases.

---

## Caching & job runtime

- **Tool cache**: a `actions/cache` step restores `./bin` keyed on
  `hashFiles('.config/tool-versions')` + runner OS, *before* `bootstrap.sh`, so the pinned
  tools are reused instead of `go install`-ed every run. A pin bump changes the key and
  forces a clean reinstall.
- **Go build/module cache**: provided by `actions/setup-go` with `cache: true` (keyed on
  `go.sum`).
- **Verification-neutral invariant**: caching only restores artifacts. The gate's pass/fail
  verdict is identical warm vs cold. A corrupt or missing cache costs reinstall time, never
  correctness.
- **Timeouts**: every job declares `timeout-minutes` (CI ≈ 15, release ≈ 30) so a hung step
  is terminated at the bound, not the platform ceiling.

---

## Dependency & pin currency (ownership partition)

No overlap between updaters — each source has exactly one owner:

| Source | Owner | Notes |
|--------|-------|-------|
| `go.mod` / `go.sum` | **Dependabot** (`gomod`) | grouped minor/patch |
| `.github/workflows/*` `uses:` SHAs | **Dependabot** (`github-actions`) | bumps the SHA **and** the trailing `# vX.Y.Z` comment |
| `.config/tool-versions` | **Renovate** (one custom manager) | the only manager Renovate runs |

Every Dependabot/Renovate PR triggers the full CI gate before it can merge.

### Enabling Renovate (required — config alone does nothing)

A committed `renovate.json` is **inert** until Renovate actually runs against the repo.
GSKILL uses the hosted **Mend Renovate GitHub App**:

1. Install the **Renovate** app from the GitHub Marketplace
   (<https://github.com/apps/renovate>) onto the `glapsfun/gskill` repository (a one-time
   repo-admin action; no secrets, no self-hosted runner).
2. Renovate reads `renovate.json`, which restricts `enabledManagers` to a single custom
   regex manager scoped to `.config/tool-versions` (so it never overlaps Dependabot's gomod
   / github-actions scope).
3. On its schedule, Renovate opens PRs that bump each tool pin while preserving the
   `NAME=version` format.

> Alternative (not used): a self-hosted SHA-pinned `renovate.yml` workflow running
> `renovatebot/github-action`. Documented for completeness; the Mend App is the chosen path.

---

## Branch protection (apply in repo settings)

These gates only block merges when branch protection requires them. Configure on `main`:

- **Required status checks**: `verify`, `build-test (ubuntu-latest)`,
  `build-test (macos-latest)`, `release-dryrun`, `workflow-audit`, `codeql`, `pr-title`.
- **Do not** mark `scorecard` required (it is informational).
- Require a pull request with at least one review; dismiss stale approvals on new commits.
- Disallow force-pushes and deletions on `main`.

Once set, OpenSSF Scorecard's Branch-Protection check will score this configuration.

---

## Forked-PR safety

- Required security gates (`workflow-audit`, `codeql`) run on the standard `pull_request`
  trigger and never receive write-scoped secrets.
- Privileged, secret-touching workflows (`scorecard` publish, `release`) run only on
  default-branch / tag / schedule contexts — they never execute forked-PR code with elevated
  scopes.
- `pr-title.yml` uses `pull_request_target` with only `pull-requests: read` (it reads the PR
  title and runs no untrusted checkout) — the documented safe use of that trigger.
