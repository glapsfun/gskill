#!/usr/bin/env bash
# Verify a published GitHub Release end-to-end for this host: download the
# archive, check it against checksums.txt, verify the cosign keyless signature
# (pinned to this tag's release.yml run) and the build-provenance attestation,
# then run the binary and confirm its version. Read-only. Arg: the tag.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need gh
need tar
tag="$(require_tag "${1:-}")"

osarch="$(host_os_arch)"
ver="${tag#v}"
archive="${RELEASE_BINARY}_${ver}_${osarch}.tar.gz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"

# 0. Asset set: four archives, four SBOMs, checksums and their signature bundle.
assets="$(gh release view "$tag" --repo "$RELEASE_REPO" --json assets --jq '.assets[].name')" ||
	die "release ${tag} not found on GitHub"
n_archives="$(printf '%s\n' "$assets" | grep -c "^${RELEASE_BINARY}_${ver}_.*\.tar\.gz$" || true)"
n_sboms="$(printf '%s\n' "$assets" | grep -c '\.sbom\.spdx\.json$' || true)"
[ "$n_archives" -eq 4 ] || die "expected 4 archives on ${tag}, found ${n_archives}"
[ "$n_sboms" -eq 4 ] || die "expected 4 SBOMs on ${tag}, found ${n_sboms}"
printf '%s\n' "$assets" | grep -qx 'checksums.txt.sigstore.json' || die "checksums.txt.sigstore.json missing on ${tag}"
log_info "asset set OK (4 archives, 4 SBOMs, checksums + signature bundle)"

log_info "downloading ${archive} + checksums ..."
gh release download "$tag" --repo "$RELEASE_REPO" --clobber \
	--pattern "$archive" --pattern "checksums.txt" --pattern "checksums.txt.sigstore.json" ||
	die "could not download release assets for ${tag}"

# 1. Checksum.
expected="$(grep " ${archive}\$" checksums.txt | awk '{print $1}')"
[ -n "$expected" ] || die "no checksum entry for ${archive} in checksums.txt"
if have sha256sum; then
	actual="$(sha256sum "$archive" | awk '{print $1}')"
elif have shasum; then
	actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
else
	die "need sha256sum or shasum to verify the checksum"
fi
[ "$expected" = "$actual" ] || die "checksum mismatch for ${archive} (expected ${expected}, got ${actual})"
log_info "checksum OK"

# 2. cosign keyless signature over checksums.txt, pinned to this tag's run of
#    release.yml (the same identity npm-release.yml enforces).
if have cosign; then
	cosign verify-blob \
		--bundle checksums.txt.sigstore.json \
		--certificate-identity "https://github.com/${RELEASE_REPO}/.github/workflows/${RELEASE_WORKFLOW}@refs/tags/${tag}" \
		--certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
		checksums.txt >/dev/null 2>&1 ||
		die "cosign signature verification FAILED for checksums.txt"
	log_info "cosign signature OK (signed by ${RELEASE_WORKFLOW}@refs/tags/${tag})"
	sig="ok"
else
	log_warn "cosign not installed; signature NOT verified (install cosign for a full check)"
	sig="skipped (no cosign)"
fi

# 3. Build-provenance attestation (subjects are the files listed in checksums.txt).
#    `gh attestation` needs gh >= 2.49; an older gh cannot check it at all, which
#    is a gap to report, not a release failure.
if ! gh attestation verify --help >/dev/null 2>&1; then
	log_warn "this gh ($(gh --version | head -1 | awk '{print $3}')) has no 'gh attestation'; provenance NOT verified (upgrade gh to >= 2.49)"
	prov="skipped (gh too old)"
elif gh attestation verify "$archive" --repo "$RELEASE_REPO" >/dev/null 2>&1; then
	log_info "provenance attestation OK"
	prov="ok"
else
	die "provenance attestation verification FAILED for ${archive} (gh attestation verify)"
fi

# 4. Smoke test: the binary runs and reports the released version.
tar -xzf "$archive" "$RELEASE_BINARY" || die "could not extract ${RELEASE_BINARY} from ${archive}"
reported="$("./${RELEASE_BINARY}" version 2>&1 | head -1)"
printf '%s' "$reported" | grep -q "$ver" ||
	die "binary version output (${reported}) does not contain ${ver}"
log_info "binary reports: ${reported}"

log_info "GitHub Release artifacts verified for ${tag} (${archive})"
echo "ASSETS=ok CHECKSUM=ok SIGNATURE=${sig// /_} PROVENANCE=${prov// /_} SMOKE=ok"
