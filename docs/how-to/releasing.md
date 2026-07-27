# Cut and verify a release

How to publish a new `gskill` version and how anyone can verify a published release.
Releases are **tag-triggered**: pushing a `vX.Y.Z` tag runs the quality gate and then
publishes everything via GoReleaser (`.github/workflows/release.yml`). After publishing
the GitHub Release, the release job dispatches the separate npm pipeline
(`.github/workflows/npm-release.yml`) — see [npm distribution](#npm-distribution).

## Prerequisites (one-time)

- **`TAP_GITHUB_TOKEN`** repo/org secret: a token with `contents:write` on
  `glapsfun/homebrew-tap` (used to push the generated cask). The release also uses the
  auto-provided `GITHUB_TOKEN`.
- Branch protection: make **`verify`** (CI) and **`Validate PR title`** required status
  checks, and enable squash-merge with "Default to PR title for squash merge commits" so
  the grouped release notes stay clean.

## Cut a stable release

1. Make sure `main` is green (the `verify` gate passed) and you are at the commit to ship.
2. Create and push an annotated tag:

   ```bash
   git tag -a v0.4.0 -m v0.4.0
   git push origin v0.4.0
   ```

3. Watch the run: `gh run watch` (or `gh run list --workflow=release.yml`).

The release job will, in order: guard a clean tree → run `scripts/bootstrap.sh` +
`scripts/verify.sh` on the tagged commit (a failed gate aborts the release) → refuse if
the release already exists → build the four archives → checksum, cosign-sign, and SBOM
them → publish the GitHub Release with grouped notes → update the Homebrew cask → attest
build provenance. Nothing is committed back to `main`.

## Cut a prerelease

Tag with an `-rc.N` suffix:

```bash
git tag -a v0.4.0-rc.1 -m v0.4.0-rc.1
git push origin v0.4.0-rc.1
```

Prereleases are flagged as GitHub pre-releases and are **excluded from the stable
channels**: the `install.sh` default and Homebrew stable are not updated (GoReleaser
`prerelease: auto` + `skip_upload: auto`).

## Release artifacts

Each release carries, for `linux`/`darwin` × `amd64`/`arm64`:

- `gskill_<version>_<os>_<arch>.tar.gz` (binary + LICENSE + README)
- `checksums.txt` and `checksums.txt.sigstore.json` (cosign bundle, cert embedded)
- `gskill_<version>_<os>_<arch>.tar.gz.sbom.spdx.json`
- a build-provenance attestation over `checksums.txt`

## Verify a published release

```bash
# integrity
curl -sSfLO https://github.com/glapsfun/gskill/releases/download/v0.4.0/checksums.txt
curl -sSfLO https://github.com/glapsfun/gskill/releases/download/v0.4.0/gskill_0.4.0_linux_amd64.tar.gz
sha256sum --ignore-missing -c checksums.txt

# signature (cosign keyless)
curl -sSfLO https://github.com/glapsfun/gskill/releases/download/v0.4.0/checksums.txt.sigstore.json
cosign verify-blob --bundle checksums.txt.sigstore.json checksums.txt

# provenance (GitHub attestation)
gh attestation verify --owner glapsfun checksums.txt
```

The `install.sh` script performs the checksum verification automatically and refuses to
install on any mismatch.

## npm distribution

Every release is also published to npm as five public scoped packages:

| Package | Contents |
| --- | --- |
| `@glapsfun/gskill` | Node launcher; the only documented package (`npx @glapsfun/gskill`) |
| `@glapsfun/gskill-darwin-x64` | `darwin/amd64` release binary (internal dependency) |
| `@glapsfun/gskill-darwin-arm64` | `darwin/arm64` release binary (internal dependency) |
| `@glapsfun/gskill-linux-x64` | `linux/amd64` release binary (internal dependency) |
| `@glapsfun/gskill-linux-arm64` | `linux/arm64` release binary (internal dependency) |

npm version `X.Y.Z` maps exactly to release tag `vX.Y.Z`. The pipeline
(`npm-release.yml`) runs **after the GitHub Release is published** (never on the tag
push): the release job dispatches it on the tag ref, because GitHub never starts
workflows from events created with the default `GITHUB_TOKEN` — a bare
`release: published` trigger would not fire for GoReleaser-created releases. It
downloads the four archives plus checksum material from that release, verifies
the cosign bundle and sha256 checksums, assembles the packages
(`npm/scripts/build-packages.mjs`), dry-run-packs all five, and publishes platform
packages first, launcher last (`npm/scripts/publish-packages.mjs`). Authentication is
npm **trusted publishing** (OIDC) with mandatory `--provenance` — there is no npm token
anywhere in the repository.

**Prerelease behavior**: a `vX.Y.Z-rc.N` release publishes to npm's `next` dist-tag and
never displaces `latest`. Stable releases publish to `latest`; a retry of an older
stable tag refuses to downgrade a newer `latest`.

**Retry**: a failed npm run never requires re-tagging or touching the GitHub Release.
Re-run for the same release with:

```bash
gh workflow run npm-release.yml --ref main -f tag=v0.6.0
```

The npm packaging sources come from the dispatched ref (`--ref`); the binaries always
come from the tag's release assets. Use `--ref main` for retries and for backfilling
tags that predate the npm channel (the workflow fails with a clear error if the
dispatched ref has no `npm/` sources).

Already-published exact versions are validated and skipped; the run converges to
all-skip when everything is out. A version that exists on only some of the five
packages after a completed run fails for operator intervention — never overwrite.

### npm trusted-publisher setup (one-time)

1. Create/confirm ownership of the `@glapsfun` npm organization.
2. Reserve all five package names above.
3. On each package, configure **Trusted Publisher**: GitHub repository
   `glapsfun/gskill`, workflow filename `npm-release.yml`.
4. Restrict publishing to the trusted publisher only.
5. Enable 2FA for all maintainers.
6. After the first successful publish, disallow token-based publishing where npm
   supports that setting.

Do **not** add an `NPM_TOKEN` or any npm credential to the repository or its secrets.

### Post-merge operations checklist (first rollout)

1. Complete the trusted-publisher setup above for all five packages.
2. Confirm CI is green on `main` (including the `npm-test` job).
3. Backfill the current latest release once:
   `gh workflow run npm-release.yml --ref main -f tag=<current latest tag>`.
4. Verify: `npx @glapsfun/gskill@<version> version` runs the native binary, and the
   npm package pages show provenance attestations.

Subsequent releases publish to npm automatically when the GitHub Release is published.

## Troubleshooting

- **Gate failed** → no release was published; fix on `main`, then re-tag (delete the tag
  locally and remotely first if you must reuse it).
- **Tap not updated** → the GitHub Release still succeeded; check the `TAP_GITHUB_TOKEN`
  secret and the GoReleaser `homebrew_casks` step logs, then re-run.
- **`tag already exists`** → the guard refuses to overwrite a published release; bump to a
  new version.
