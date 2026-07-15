# Cut and verify a release

How to publish a new `gskill` version and how anyone can verify a published release.
Releases are **tag-triggered**: pushing a `vX.Y.Z` tag runs the quality gate and then
publishes everything via GoReleaser (`.github/workflows/release.yml`).

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

## Troubleshooting

- **Gate failed** → no release was published; fix on `main`, then re-tag (delete the tag
  locally and remotely first if you must reuse it).
- **Tap not updated** → the GitHub Release still succeeded; check the `TAP_GITHUB_TOKEN`
  secret and the GoReleaser `homebrew_casks` step logs, then re-run.
- **`tag already exists`** → the guard refuses to overwrite a published release; bump to a
  new version.
