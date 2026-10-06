# Failure playbook

Read the section for the stage that failed. Every section answers three questions: what is
already public, what is safe to do, and what needs the user. When in doubt, run
`scripts/release-status.sh <tag>` first; it is read-only and shows the real state of every stage,
which is what decides the action (a run's red/green status alone does not).

Two rules hold everywhere: never delete, move, or force-push a tag that reached origin unless
section C's narrow exception applies and the user agreed; and never use `npm unpublish`,
`npm dist-tag`, `gh release delete|edit`, or hand edits to the Homebrew tap to "fix" a release.
Those are maintainer decisions with public consequences.

## Contents

- A. Preflight failed
- B. Tag push failed
- C. Release run failed before anything was published
- D. GoReleaser failed, or published only part of the release
- E. Provenance attestation step failed
- F. "Trigger npm publish" step failed
- G. npm run failed
- H. Verification failed after green runs
- I. A bad release is discovered later

## A. Preflight failed

Public: nothing. Fix the cause and re-run preflight. Not on `main` means the change isn't merged;
an open PR can't be released. CI not green for the commit: wait for it, or fix `main` first. Gate
red locally: fix on a branch, merge, and start over from step 1.

## B. Tag push failed

Public: nothing (`cut-release.sh` deleted the local tag). Usual causes: gh/git credentials or a
network error. Fix and re-run step 4 with the same approved SHA, after confirming
`origin/main` hasn't moved (the script checks).

## C. Release run failed before anything was published

Failed step is one of: `Guard — clean working tree`, `Set up Go`, `Quality gate`, `Guard —
refuse to overwrite an existing release`, `Install cosign`, `Install syft`. Confirm with
`gh release view <tag>`: it must say not found.

Public: the tag only. No GitHub Release, no Homebrew change, no npm packages.

- **Infrastructure flake** (network timeout, runner error, action download failure, with the
  gate itself not reporting a test failure): re-run the failed job once, `gh run rerun <run-id>
  --failed`, then go back to step 5.
- **Real failure** (the gate found a bug, or `go mod tidy` drift): fix on `main` through a normal
  PR. Then ask the user which way to go:
  - Recommended: release the next patch version (`vX.Y.Z+1`) from the fixed `main`. The failed tag
    stays as a harmless orphan pointing at an unreleased commit.
  - Reuse the version: only because nothing was published, and only with the user's explicit OK:
    `git push origin :refs/tags/<tag>` and `git tag -d <tag>`, then run the runbook again from
    step 2 for the same tag. (`docs/how-to/releasing.md` allows this case; it is never allowed
    once a GitHub Release exists.)
- `Guard — refuse to overwrite` failing means a release with this tag already exists: someone
  else released it. Stop and ask the user; never touch that release.

## D. GoReleaser failed, or published only part of the release

Check `gh release view <tag>` and `scripts/release-status.sh <tag>`.

- **No GitHub Release exists**: GoReleaser failed before publishing (build, sign, or SBOM error).
  Treat it like section C, real failure.
- **The GitHub Release exists** (typically the Homebrew cask push failed, which runs after the
  release is created): the release is public. Don't re-run the job; the overwrite guard refuses,
  and the release must not be rewritten. The npm dispatch, the last step, did not run either.
  Tell the user, and offer:
  1. The npm channel can still be completed: `gh workflow run npm-release.yml --ref main -f
     tag=<tag>` (it reads the published assets, verifies cosign, and is retry-safe). Provenance
     did not run either; say so (section E).
  2. Homebrew: the likely cause is the `TAP_GITHUB_TOKEN` secret (expired, or lost `contents:
     write` on `glapsfun/homebrew-tap`). Once fixed, the next stable release updates the cask
     again; updating the cask now by hand is the maintainer's call, not yours.

## E. Provenance attestation step failed

Public: the GitHub Release with all assets and signatures, and the Homebrew cask (stable). Missing:
the provenance attestation, and npm (its dispatch is the step after).

Tell the user the release is out without provenance. With their OK, complete npm with
`gh workflow run npm-release.yml --ref main -f tag=<tag>` (npm verifies the cosign signature, not
the attestation), then continue at step 6. Attesting after the fact is a maintainer task.

## F. "Trigger npm publish" step failed

Public: the GitHub Release, signatures, provenance, and Homebrew cask. Only npm is missing.

This is completing the release the user already approved, and the npm workflow is retry-safe, so
dispatch it: `gh workflow run npm-release.yml --ref main -f tag=<tag>`. Then continue at step 6
and mention the manual dispatch in the report.

## G. npm run failed

First read the registry state: `scripts/release-status.sh <tag>` (the npm packages row and
dist-tags). Then:

- **5/5 published and the dist-tag is right** (`latest` = version for stable, `next` for rc): npm
  is fine; the run failed after publishing (v0.7.2 did this, in its publish step). Report it as
  published, noting the red run. Optionally re-dispatch to get a green run; already-published
  versions are skipped, so the re-run converges without changing anything.
- **0/5 published**: nothing reached npm. Read the failed log (`gh run view <id> --log-failed`).
  Common causes: npm trusted-publisher configuration (needs a maintainer in npm's settings), npm
  older than 11.5.1, or a registry outage. After the cause is fixed, re-dispatch:
  `gh workflow run npm-release.yml --ref main -f tag=<tag>`. Never add an `NPM_TOKEN`.
- **Partial (1-4 of 5)**: re-dispatch once; the publisher skips existing versions and publishes the
  rest. If a completed run still leaves the set partial, stop: that needs operator intervention,
  and nothing may be overwritten or unpublished. Ask the user.
- **Dist-tag wrong** (stable not on `latest`, or an rc on `latest`): stop and ask the user;
  changing dist-tags is a maintainer action. Note the publisher refuses to move `latest` backwards
  when retrying an older stable tag, by design.

Dispatching from `--ref main` is correct for retries: packaging code comes from the ref, binaries
always come from the tag's release assets.

## H. Verification failed after green runs

- **Checksum mismatch, bad cosign signature, or failed provenance**: treat as a possible supply-
  chain problem. Stop, tell the user immediately, and ask them not to announce the release.
  Don't delete or change anything; the evidence matters.
- **Binary reports the wrong version**: the build stamped the wrong tag (see
  `GORELEASER_CURRENT_TAG` in `release.yml`; it matters when a stable tag and its rc share a
  commit). Report to the user; the fix is a new patch release.
- **Homebrew cask not at the new stable version**: section D, Homebrew part.
- **install.sh default does not resolve to the new version**: check that the release is not
  marked prerelease or draft (`gh release view <tag>`) and what `gh api
  repos/glapsfun/gskill/releases/latest` returns; report to the user.
- **`skipped` checks** (no cosign, gh older than 2.49): not failures; name them in the report and
  suggest installing the tool and re-running the verifier.

## I. A bad release is discovered later

Never delete or move the tag, delete the GitHub Release, or unpublish npm packages: installs and
lockfiles already point at them. Fix the bug on `main` and ship a new patch release through this
runbook. Deprecating the bad npm version (`npm deprecate`) or editing the release notes to warn
users are maintainer decisions; suggest them, don't do them.
