#!/usr/bin/env bash
# Verify a release on every install channel besides the GitHub assets:
#   npm      all five packages at the version, each with SLSA provenance, the
#            right dist-tag (latest for stable, next for -rc), and an npx smoke run
#   Homebrew the tap's cask points at the version (stable) or is untouched (-rc)
#   image    ghcr.io/glapsfun/gskill: the version tag resolves anonymously, the index
#            holds both platforms, the channel tags follow the tag policy, the
#            release notes record the digest, the signature and provenance verify,
#            and the image reports the version
#   install  scripts/install.sh from the tag installs a binary reporting the version
# Read-only (installs only into temp dirs). Checks every channel and reports all
# failures at the end; a check whose tool is missing is reported as skipped.
# Arg: the tag.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need gh
need curl
tag="$(require_tag "${1:-}")"
ver="${tag#v}"
pre=0
is_prerelease "$tag" && pre=1

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fails=()
fail() { log_error "$*"; fails+=("$*"); }
skips=()
skip() { log_warn "$*"; skips+=("$*"); }

# newest_stable prints the tag GitHub currently marks as the latest release.
latest_tag="$(gh api "repos/${RELEASE_REPO}/releases/latest" --jq .tag_name 2>/dev/null || true)"
superseded=0
if [ "$pre" = 0 ] && [ -n "$latest_tag" ] && [ "$latest_tag" != "$tag" ]; then
	superseded=1
	log_warn "${tag} is not the latest stable release (${latest_tag} is); channel 'latest' checks expect ${latest_tag}"
fi

# --- npm ----------------------------------------------------------------------
if have npm; then
	for pkg in $NPM_PACKAGES; do
		got="$(npm view "${pkg}@${ver}" version 2>/dev/null || true)"
		if [ "$got" != "$ver" ]; then
			fail "npm: ${pkg}@${ver} is not published"
			continue
		fi
		prov="$(npm view "${pkg}@${ver}" dist.attestations.provenance.predicateType 2>/dev/null || true)"
		case "$prov" in
		*slsa.dev/provenance*) log_info "npm: ${pkg}@${ver} published with provenance" ;;
		*) fail "npm: ${pkg}@${ver} has no provenance attestation" ;;
		esac
	done
	tags="$(npm view "$NPM_LAUNCHER" dist-tags --json 2>/dev/null || echo '{}')"
	latest_npm="$(printf '%s' "$tags" | sed -n 's/.*"latest": *"\([^"]*\)".*/\1/p')"
	next_npm="$(printf '%s' "$tags" | sed -n 's/.*"next": *"\([^"]*\)".*/\1/p')"
	if [ "$pre" = 1 ]; then
		[ "$next_npm" = "$ver" ] || fail "npm: dist-tag next is '${next_npm}', expected ${ver}"
		[ "$latest_npm" != "$ver" ] || fail "npm: prerelease ${ver} took dist-tag latest"
		log_info "npm: dist-tags latest=${latest_npm} next=${next_npm}"
	elif [ "$superseded" = 1 ]; then
		log_info "npm: dist-tag latest=${latest_npm} (superseded release; not checked against ${ver})"
	else
		[ "$latest_npm" = "$ver" ] || fail "npm: dist-tag latest is '${latest_npm}', expected ${ver}"
		log_info "npm: dist-tag latest=${latest_npm}"
	fi
	if have npx; then
		out="$(cd "$tmp" && npm_config_cache="$tmp/npm-cache" npx -y "${NPM_LAUNCHER}@${ver}" version 2>&1 | head -1 || true)"
		case "$out" in
		*"$ver"*) log_info "npm: npx ${NPM_LAUNCHER}@${ver} reports: ${out}" ;;
		*) fail "npm: npx ${NPM_LAUNCHER}@${ver} did not report ${ver} (got: ${out})" ;;
		esac
	fi
else
	fail "npm: npm is not installed; npm channel not verified"
fi

# --- Homebrew -----------------------------------------------------------------
cask="$(gh api "repos/${TAP_REPO}/contents/${TAP_CASK_PATH}" --jq .content 2>/dev/null | base64 -d 2>/dev/null || true)"
cask_ver="$(printf '%s\n' "$cask" | sed -n 's/^[[:space:]]*version "\(.*\)"/\1/p' | head -1)"
if [ -z "$cask_ver" ]; then
	fail "homebrew: could not read ${TAP_REPO}/${TAP_CASK_PATH}"
elif [ "$pre" = 1 ]; then
	[ "$cask_ver" != "$ver" ] || fail "homebrew: prerelease ${ver} was pushed to the stable cask"
	log_info "homebrew: cask stays at ${cask_ver} (prereleases skip the tap)"
elif [ "$superseded" = 1 ]; then
	log_info "homebrew: cask at ${cask_ver} (superseded release; not checked against ${ver})"
else
	[ "$cask_ver" = "$ver" ] || fail "homebrew: cask is at ${cask_ver}, expected ${ver} (check the TAP_GITHUB_TOKEN secret and the homebrew_casks step)"
	log_info "homebrew: cask at ${cask_ver}"
fi

# --- container image ----------------------------------------------------------
# The registry checks run the repository's scripts/image.sh, anonymously, so a
# private package fails here the way it fails for users.
image_sh="$(git rev-parse --show-toplevel 2>/dev/null || true)/scripts/image.sh"
image_digest() { "$image_sh" digest "${IMAGE_REPO}:$1" 2>/dev/null || true; }
status=1
# Only a 404 means the tag predates the image channel; any other error must not
# skip the checks.
if ko_err="$(gh api "repos/${RELEASE_REPO}/contents/.ko.yaml?ref=${tag}" --silent 2>&1)"; then
	if [ ! -x "$image_sh" ]; then
		fail "image: scripts/image.sh not found; run this from a gskill checkout"
	else
		status=0
		index="$("$image_sh" digest "${IMAGE_REPO}:${ver}" 2>"$tmp/image.err")" || status=$?
		case "$status" in
		0) log_info "image: ${IMAGE_REPO}:${ver} is ${index}" ;;
		1) fail "image: ${IMAGE_REPO}:${ver} does not exist" ;;
		*) fail "image: ${IMAGE_REPO}:${ver}: package private or registry error; see the playbook ($(tail -n1 "$tmp/image.err"))" ;;
		esac
	fi
elif [[ "$ko_err" == *"HTTP 404"* ]]; then
	log_info "image: ${tag} has no .ko.yaml; it predates the image channel, which is not back-filled"
else
	fail "image: could not check ${tag} for .ko.yaml (${ko_err}); the image was NOT verified"
fi
if [ "$status" = 0 ]; then
	plats="$("$image_sh" platforms "${IMAGE_REPO}@${index}" 2>/dev/null | awk '{ print $1 }' | paste -sd' ' - || true)"
	[ "$plats" = "linux/amd64 linux/arm64" ] ||
		fail "image: the index holds '${plats}', expected linux/amd64 linux/arm64"

	# The tag policy publish applies, against the tags on origin now: a stable
	# release holds latest and a prerelease next, until a newer one of its kind.
	if [ "$pre" = 1 ]; then
		channel=next other=latest
	else
		channel=latest other=next
	fi
	[ "$(image_digest "$other")" != "$index" ] || fail "image: ${ver} took ${other}"
	channel_img="$(image_digest "$channel")"
	expected="$("$image_sh" tags "$tag" 2>"$tmp/image.err" || true)"
	case ",${expected}," in
	,,) fail "image: could not compute the expected tags ($(tail -n1 "$tmp/image.err"))" ;;
	*",${channel},"*) [ "$channel_img" = "$index" ] || fail "image: ${channel} is '${channel_img}', expected ${index}" ;;
	*)
		[ "$channel_img" != "$index" ] ||
			fail "image: ${channel} still points at ${ver}, though a newer ${channel} release exists"
		log_info "image: ${channel} is on a newer release (superseded)"
		;;
	esac

	recorded="$(gh release view "$tag" --repo "$RELEASE_REPO" --json body --jq .body 2>/dev/null |
		awk '/<!-- container-image:start -->/ { on = 1 } on' | grep -o 'sha256:[0-9a-f]\{64\}' | head -n1 || true)"
	[ "$recorded" = "$index" ] ||
		fail "image: the release notes record '${recorded:-no digest}', expected ${index}"

	if have cosign; then
		if cosign verify "${IMAGE_REPO}@${index}" \
			--certificate-identity "https://github.com/${RELEASE_REPO}/.github/workflows/${IMAGE_WORKFLOW}@refs/tags/${tag}" \
			--certificate-oidc-issuer "https://token.actions.githubusercontent.com" >/dev/null 2>&1; then
			log_info "image: cosign signature OK (signed by ${IMAGE_WORKFLOW}@refs/tags/${tag})"
		else
			fail "image: cosign signature verification FAILED for ${IMAGE_REPO}@${index}"
		fi
	else
		skip "image: cosign not installed; the image signature was NOT verified"
	fi

	if ! gh attestation verify --help >/dev/null 2>&1; then
		skip "image: gh $(gh --version | head -1 | awk '{print $3}') has no 'gh attestation'; the image provenance was NOT verified (upgrade gh to >= 2.49)"
	elif gh attestation verify "oci://${IMAGE_REPO}@${index}" --repo "$RELEASE_REPO" >/dev/null 2>&1; then
		log_info "image: provenance attestation OK"
	else
		fail "image: provenance attestation verification FAILED for ${IMAGE_REPO}@${index}"
	fi

	if have docker && docker info >/dev/null 2>&1; then
		out="$(docker run --rm "${IMAGE_REPO}@${index}" version 2>&1 | head -1 || true)"
		if [ "$out" = "gskill ${ver}" ]; then
			log_info "image: reports ${out}"
		else
			fail "image: reports '${out}', expected gskill ${ver}"
		fi
	else
		skip "image: docker not available; the version the image reports was NOT checked"
	fi
fi

# --- install.sh ---------------------------------------------------------------
installer="https://raw.githubusercontent.com/${RELEASE_REPO}/${tag}/scripts/install.sh"
if curl -sSfL "$installer" -o "$tmp/install.sh"; then
	mkdir -p "$tmp/pinned"
	if VERSION="$tag" INSTALL_DIR="$tmp/pinned" sh "$tmp/install.sh" >/dev/null 2>&1 &&
		"$tmp/pinned/${RELEASE_BINARY}" version 2>&1 | head -1 | grep -q "$ver"; then
		log_info "install.sh: VERSION=${tag} installs a binary reporting ${ver}"
	else
		fail "install.sh: VERSION=${tag} install failed or reported the wrong version"
	fi
	if [ "$pre" = 0 ] && [ "$superseded" = 0 ]; then
		mkdir -p "$tmp/default"
		if INSTALL_DIR="$tmp/default" sh "$tmp/install.sh" >/dev/null 2>&1 &&
			"$tmp/default/${RELEASE_BINARY}" version 2>&1 | head -1 | grep -q "$ver"; then
			log_info "install.sh: default (latest) install resolves to ${ver}"
		else
			fail "install.sh: default install does not resolve to ${ver}"
		fi
	fi
else
	fail "install.sh: could not download ${installer}"
fi

if [ "${#skips[@]}" -gt 0 ]; then
	log_warn "${#skips[@]} check(s) skipped for ${tag}; a skip is not a pass:"
	printf '  - %s\n' "${skips[@]}" >&2
fi
if [ "${#fails[@]}" -gt 0 ]; then
	log_error "${#fails[@]} channel check(s) failed for ${tag}:"
	printf '  - %s\n' "${fails[@]}" >&2
	echo "CHANNELS=failed"
	exit 1
fi
log_info "all channels verified for ${tag}"
echo "CHANNELS=ok"
