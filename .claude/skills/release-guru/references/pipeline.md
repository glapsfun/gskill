# The gskill release pipeline

Background for the runbook: what each stage does, what it produces, and what must already be set
up. Sources of truth: `.github/workflows/release.yml`, `.github/workflows/npm-release.yml`,
`.goreleaser.yaml`, and `docs/how-to/releasing.md`.

## Stage 1: release.yml (on push of a `v*` tag)

Runs on `ubuntu-latest`, 30-minute timeout, in the `release` concurrency group (releases queue,
never run in parallel). Steps, in order:

1. Checkout with full history and tags.
2. Guard: clean working tree.
3. Set up Go 1.26 with caching disabled (a publish job must not consume a poisonable cache).
4. Quality gate: `./scripts/bootstrap.sh` then `./scripts/verify.sh` on the tagged commit.
5. Guard: refuse if a GitHub Release for the tag already exists.
6. Install cosign and syft.
7. GoReleaser `release --clean`, with `GORELEASER_CURRENT_TAG` pinned to the tag (so a stable
   tag and an rc on the same commit can't be confused):
   - builds `gskill` for linux/darwin × amd64/arm64 (CGO off, `-trimpath`, version and commit
     stamped through ldflags);
   - archives `gskill_<version>_<os>_<arch>.tar.gz` (binary, LICENSE, README). This name is a
     hard contract with `scripts/install.sh` and the Homebrew cask;
   - `checksums.txt` (sha256) and `checksums.txt.sigstore.json` (cosign keyless bundle; the
     signing certificate is embedded, identity `release.yml@refs/tags/<tag>`);
   - one SPDX SBOM per archive (`<archive>.sbom.spdx.json`);
   - the GitHub Release with notes grouped into Features (`feat`), Bug Fixes (`fix`), and
     Others; subjects starting `docs:`, `test:`, `chore:`, `ci:`, or `Merge ` are dropped.
     `prerelease: auto` flags `-rc` tags;
   - the Homebrew cask `Casks/gskill.rb` in `glapsfun/homebrew-tap`, pushed with
     `TAP_GITHUB_TOKEN` (`skip_upload: auto` keeps prereleases out).
8. Build-provenance attestation, with `checksums.txt` as the subject list (covers every archive).
9. Dispatch `npm-release.yml` on the tag ref with `tag=<tag>`. This is an explicit dispatch
   because releases created with the default `GITHUB_TOKEN` never trigger other workflows.

Nothing is committed back to `main`; there is no CHANGELOG file.

## Stage 2: npm-release.yml (dispatched, or manual retry)

Inputs: `tag`. Runs in the `npm-release` concurrency group, 20-minute timeout. It validates the
tag shape, checks out the dispatched ref (the tag for automatic runs, normally `main` for manual
ones), requires npm >= 11.5.1, downloads the four archives plus checksum material from the
**tag's release**, verifies the cosign bundle against `release.yml@refs/tags/<tag>` and the
sha256 checksums, builds the five packages, dry-run packs them, and publishes platform packages
first and the launcher last.

| Package | Contents |
| --- | --- |
| `@glapsfun/gskill` | Node launcher (`npx @glapsfun/gskill`) |
| `@glapsfun/gskill-darwin-x64` | darwin/amd64 binary |
| `@glapsfun/gskill-darwin-arm64` | darwin/arm64 binary |
| `@glapsfun/gskill-linux-x64` | linux/amd64 binary |
| `@glapsfun/gskill-linux-arm64` | linux/arm64 binary |

npm version `X.Y.Z` is exactly tag `vX.Y.Z`. Stable versions get dist-tag `latest`, `-rc.N`
gets `next`. Already-published versions are validated and skipped, so re-runs converge. Publishing
uses npm trusted publishing (OIDC) with mandatory provenance; there is no npm token anywhere.

## One-time prerequisites (maintainer setup, not runbook steps)

- Secret `TAP_GITHUB_TOKEN` with `contents: write` on `glapsfun/homebrew-tap`.
- Branch protection on `main` requiring the `verify` check and `Validate PR title`.
- npm: the `@glapsfun` org owns all five packages, each with a Trusted Publisher of repository
  `glapsfun/gskill` and workflow `npm-release.yml`, token publishing disallowed, 2FA for
  maintainers.

If a run fails on one of these, the fix belongs to a maintainer; report it rather than working
around it.

## Versioning

Semantic versioning on tags `vX.Y.Z`, prereleases `vX.Y.Z-rc.N`. Pull requests are merged with
merge commits whose body is the PR title, and `pr-title.yml` enforces conventional titles, so
merged PR titles are the reliable record of what changed. The project is pre-1.0: breaking
changes bump the minor.

## Verifying by hand

```bash
gh release download vX.Y.Z --pattern 'checksums.txt*' --pattern 'gskill_X.Y.Z_linux_amd64.tar.gz'
sha256sum --ignore-missing -c checksums.txt
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity 'https://github.com/glapsfun/gskill/.github/workflows/release.yml@refs/tags/vX.Y.Z' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
gh attestation verify gskill_X.Y.Z_linux_amd64.tar.gz --repo glapsfun/gskill   # gh >= 2.49
npm view @glapsfun/gskill@X.Y.Z dist.attestations.provenance.predicateType
```
