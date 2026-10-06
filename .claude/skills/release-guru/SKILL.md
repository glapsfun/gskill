---
name: release-guru
description: >-
  The release runbook for gskill. Use it for every gskill release and for anything about a
  release in flight: the moment a user wants to release, ship, publish, cut, tag, or roll out a
  gskill version ("ship v0.8.0", "cut a release", "tag and publish", "do a patch/minor/rc
  release", "release off main", a bare "ship it", /release-guru), and also when a release needs
  checking, resuming, or rescuing ("is v0.8.0 out everywhere?", "the release run went red",
  "npm publish failed", "the brew cask didn't update", "verify the release"). STOP and use it
  before touching git tags or gh: a hand-made tag, push, gh release, or npm publish skips the
  quality gate and leaves an unsigned, unverifiable release with no SBOMs, provenance, Homebrew
  update, or npm packages. It picks the semver version from merged PR titles, runs preflight
  (identity, branch, CI green on the exact commit, local gate), gets the user's one go-ahead,
  pushes the tag, watches the GitHub Release and npm pipelines, verifies every install channel
  (GitHub assets, cosign, provenance, npm, Homebrew, install.sh), and has a failure playbook for
  each stage. Skip it only for work that merely mentions releases: editing release.yml or
  .goreleaser.yaml, hand-writing a changelog, bumping a go.mod dependency, a local-only git tag,
  scaffolding GoReleaser for another project, or checking a pull request's CI.
compatibility: Requires git, bash, curl, and the GitHub CLI (gh, authenticated). npm/npx verify the npm channel; cosign and gh >= 2.49 (gh attestation) complete signature and provenance checks, and their absence is reported, not hidden.
---

# release-guru: the gskill release runbook

You are about to publish software to everyone who installs gskill. A release is **public and
irreversible**: pushing a `vX.Y.Z` tag starts a pipeline that publishes signed binaries, a
GitHub Release, a Homebrew cask, and five npm packages, and none of it can be cleanly taken
back. This runbook keeps exactly one decision with the human (which version, from which
commit) and makes everything else scripted, watched, and verified.

Run the scripts from this skill's `scripts/` directory; they find the repo through git.

## The pipeline in one minute

```
git tag vX.Y.Z (pushed)                         ← point of no return
 └─ release.yml (8-12 min)
     guard clean tree → bootstrap + verify.sh gate on the tag → refuse if release exists
     → GoReleaser: 4 archives (linux/darwin × amd64/arm64), checksums.txt,
       cosign bundle, 4 SBOMs, GitHub Release with grouped notes,
       Homebrew cask in glapsfun/homebrew-tap (stable only)
     → build-provenance attestation → dispatch npm-release.yml on the tag
 └─ npm-release.yml (1-3 min)
     verify cosign + checksums of the release assets → build 5 packages
     → publish platforms first, launcher last, with provenance
       (stable → dist-tag latest, -rc.N → dist-tag next)
```

Prereleases (`-rc.N`) become GitHub pre-releases, go to npm `next`, and never touch Homebrew or
the `install.sh` default. Details, prerequisites, and artifact names: `references/pipeline.md`.

## Ground rules

- **One human gate.** The user confirms the version and commit in step 3. Never tag without
  that explicit yes, and never treat an earlier "ship it" as approval for a version they have not
  seen.
- **Only the scripts touch the release.** Don't hand-run `git tag`/`git push` of tags,
  `gh release create|edit|delete`, `npm publish|unpublish|dist-tag`, or edit the Homebrew tap.
  The scripts encode the guards; a manual step skips them.
- **Tags and releases are immutable.** Never delete, move, or force-push a tag that reached
  origin, and never re-run the Release workflow once a GitHub Release exists (its guard refuses,
  and forcing around it would rewrite a published release). Fixes ship as a new version. The one
  narrow exception (a tag whose run published nothing) is in the failure playbook, and it needs
  the user's say-so.
- **Red is a stop sign, not a puzzle to route around.** When a step fails, classify it with
  `references/failure-playbook.md` before doing anything. Some failures are safe to retry; the
  ones marked "ask the user" go to the user.
- **Right identity.** Every tag push and gh call acts as the active gh account. Preflight
  checks it matches the git user; if not, run `gh auth switch --user <git user.name>` and
  confirm with `gh auth status`.

## Runbook

### 0. Orient: is a release already in flight?

If the user names a version that may already be tagged, or is asking about a release rather
than for one, start with the read-only status board:

```bash
scripts/release-status.sh vX.Y.Z
```

It shows the tag, the Release run, the GitHub Release, the npm run, npm packages and dist-tags,
the Homebrew cask, and which release GitHub marks latest. Resume at the first stage that is not
done: tag missing → step 1; Release run running → step 5; Release run failed → playbook;
GitHub Release exists but npm incomplete → step 6; everything published → step 7. For "is it
healthy?" questions, step 7 alone is the answer.

### 1. Choose the version

```bash
scripts/suggest-version.sh
```

It compares the latest `v*` tag with `origin/main` (what a release would tag) and derives the
bump from merged PR titles and commit subjects: any `type!:` or `BREAKING CHANGE` → major,
any `feat:` → minor, otherwise patch. Read its list and apply judgment:

- **Pre-1.0:** a breaking change bumps the minor (`v0.7.2` → `v0.8.0`); the script suggests that
  and prints `ALTERNATIVE=v1.0.0`. Going to 1.0.0 is a product decision only the user makes.
- **Prerelease:** for an rc, append `-rc.N` (`v0.8.0-rc.1`); count up N from any existing rc tags.
- **Nothing to release** (`BUMP=none`) or only `build(deps)`/`chore`/`docs`/`ci`/`test` changes:
  say so and ask whether a release is wanted at all; those prefixes are filtered out of the
  release notes, so the notes would be nearly empty.
- If it warns that local HEAD differs from `origin/main`, the suggestion still describes
  `origin/main`; unmerged local work is not part of the release.

### 2. Preflight

```bash
scripts/preflight.sh vX.Y.Z      # prints SHA=<commit> on success
```

It checks, failing closed: gh identity matches git; on `main`, clean, in sync with origin; tag and
release don't exist yet; no Release run in progress; **CI green for this exact commit** (CI
includes `release-dryrun`, a GoReleaser snapshot of what would ship); and the local
`./scripts/verify.sh` gate (several minutes). It also warns early when cosign, a gh with
`gh attestation`, or npm is missing, because step 7 would then report those checks as skipped.

Typical fixes: not on `main` → the work must be merged first (open PRs are not releasable); CI
still running → wait for it; out of sync → `git pull --ff-only`. Re-run preflight after any fix.

### 3. Confirm with the user (the gate)

Present this and wait for an explicit yes:

```
Release vX.Y.Z from main @ <short sha> (<commit subject>)
Bump: <level>, because <the PR titles that drove it>
Publishes: GitHub Release + signed archives, Homebrew cask, npm @glapsfun/gskill (latest)
           [for -rc: GitHub pre-release, npm next only; no Homebrew]
Notes preview: <Features / Bug Fixes items from the PR titles>
Verification gaps on this machine: <none | cosign missing | gh too old for provenance>
Tagging is irreversible. Go ahead?
```

An override from the user (a different version) means re-running preflight for that tag.

### 4. Cut the tag

```bash
scripts/cut-release.sh vX.Y.Z <SHA from preflight>
```

It re-checks identity and that `origin/main` still equals the approved SHA, then creates the
annotated tag on that commit and pushes it. If the push fails it deletes the local tag so a retry
starts clean (nothing public happened).

### 5. Watch the Release run

```bash
scripts/watch-release.sh vX.Y.Z
```

Takes 8-12 minutes; run it in the background or with a long timeout rather than polling by hand.
On failure it prints the failed step and log tail. Go straight to the playbook: what is safe
depends entirely on whether the failed step came before or after GoReleaser published.

### 6. Watch the npm run

```bash
scripts/watch-npm.sh vX.Y.Z
```

On failure it prints the registry state. A red npm run does not by itself mean npm is broken:
v0.7.2's run failed after all five packages were already published. Decide from the registry
state using the playbook's npm section.

### 7. Verify every channel

```bash
scripts/verify-artifacts.sh vX.Y.Z   # GitHub assets: set, checksum, cosign, provenance, binary
scripts/verify-channels.sh vX.Y.Z    # npm (5 pkgs, provenance, dist-tag, npx), Homebrew, install.sh
```

Both are read-only. A failure here after green runs is serious (see playbook section H): stop and
tell the user before anyone announces the release. A check reported as `skipped` is not a pass;
name it in the report.

### 8. Report

```
Released vX.Y.Z  <GitHub Release URL>
Commit:      <short sha> on main
Pipelines:   Release run <url> (success) · npm run <url> (success | failed, see note)
GitHub:      4 archives + SBOMs · checksum ok · signature <ok|skipped> · provenance <ok|skipped> · binary reports X.Y.Z
npm:         5/5 packages with provenance · dist-tag <latest|next> = X.Y.Z · npx smoke ok
Homebrew:    cask at X.Y.Z  [rc: untouched, by design]
install.sh:  pinned install ok · default install resolves to X.Y.Z  [stable only]
Notes:       <Features / Bug Fixes headlines>
Follow-ups:  <anything skipped or retried, and why>
```

## Quick reference

| Step | Command | Stops when |
| --- | --- | --- |
| Status board | `scripts/release-status.sh <tag>` | never (read-only) |
| Version | `scripts/suggest-version.sh` | never (informational) |
| Preflight | `scripts/preflight.sh <tag>` | identity, branch, dirty, sync, tag exists, release in flight, CI not green, gate red |
| Tag | `scripts/cut-release.sh <tag> <sha>` | identity, main moved, tag appeared, push rejected |
| Release run | `scripts/watch-release.sh <tag>` | run fails |
| npm run | `scripts/watch-npm.sh <tag>` | run fails (prints registry state) |
| Verify GitHub | `scripts/verify-artifacts.sh <tag>` | asset set, checksum, signature, provenance, version mismatch |
| Verify channels | `scripts/verify-channels.sh <tag>` | any channel wrong (reports all) |

## Configuration (environment overrides)

`RELEASE_BRANCH` (default `main`), `RELEASE_REPO` (`glapsfun/gskill`), `RELEASE_WORKFLOW`
(`release.yml`), `NPM_WORKFLOW` (`npm-release.yml`), `CI_WORKFLOW` (`ci.yml`), `TAP_REPO`
(`glapsfun/homebrew-tap`), `NPM_PACKAGES`, and `RELEASE_GURU_SKIP_GATE=1` (skip the local gate in
preflight; only when the gate already passed on this exact commit, and say so in the report).

## Rehearsing

This skill always publishes for real; success means a real, verified release. To rehearse, rely
on the `release-dryrun` CI job, or run `goreleaser release --snapshot --clean --skip=sign,sbom`
locally. `scripts/release-status.sh`, `verify-artifacts.sh`, and `verify-channels.sh` are safe to
run against any existing release at any time.
