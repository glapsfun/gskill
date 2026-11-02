#!/usr/bin/env bash
# Container image helper. ko builds the image from the Go module, so building
# needs no Docker daemon; only running the image does.
#
#   image.sh build              host-platform image to dist/image/gskill.tar
#   image.sh build --all        both platforms to the OCI layout dist/image/oci
#   image.sh check              what the CI image job runs: build both platforms,
#                               load the host one into Docker, smoke-test it
#   image.sh smoke REF [--version V]
#                               the smoke suite against an image, as your UID
#   image.sh tags TAG [HIGHEST] registry tags for a release tag; HIGHEST is the
#                               highest tag of its kind, read from origin if absent
#   image.sh digest REF         index digest of a registry reference
#   image.sh platforms REF      one "os/arch digest bytes" line per platform
#   image.sh publish TAG        CI only: build, guard, and push a release image
#
# Building needs Go and ko (scripts/bootstrap.sh); smoke and check need Docker
# and jq; digest and platforms need curl and jq; publish also needs crane. Docs:
# docs/contributing/development.md.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cd "$repo_root"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/glapsfun/gskill}"
# Subcommands that call each other run as a new process: bash turns set -e off
# inside $(...) and inside anything tested with ||.
self="${repo_root}/scripts/image.sh"

usage() {
	log_error "usage: $(basename "$0") build [--all] | check | smoke REF [--version V] | tags TAG HIGHEST | digest REF | platforms REF | publish TAG"
	exit 2
}

host_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported architecture $(uname -m): images exist for amd64 and arm64" ;;
	esac
}

# ko build with the labels, annotations, and SBOM setting every image gets. The
# annotations land on the index and on each platform manifest.
ko_build() {
	local source="https://github.com/glapsfun/gskill"
	local description="Reproducible package manager for agentic AI skills"
	ko build ./cmd/gskill --bare --sbom=none \
		--image-label "org.opencontainers.image.source=${source}" \
		--image-label "org.opencontainers.image.version=${GSKILL_VERSION}" \
		--image-label "org.opencontainers.image.revision=$(git rev-parse HEAD)" \
		--image-label "org.opencontainers.image.title=gskill" \
		--image-label "org.opencontainers.image.description=${description}" \
		--image-label "org.opencontainers.image.licenses=MIT" \
		--image-annotation "org.opencontainers.image.source=${source}" \
		--image-annotation "org.opencontainers.image.description=${description}" \
		--image-annotation "org.opencontainers.image.licenses=MIT" \
		"$@"
}

# Local builds never use a ko.local repository: ko sends those to the Docker
# daemon and ignores --tarball and --push=false. Nothing can be pushed to the
# reserved .test domain by mistake. gskill reads an injected "dev" as unset and
# falls back to the module version, which Go stamps from git as a
# pseudo-version; without VCS stamping it stays "dev". ko reads the commit from
# git itself.
local_build_env() {
	need_tool ko
	export GSKILL_VERSION=dev KO_DOCKER_REPO=gskill.test/gskill
	export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-buildvcs=false"
}

build() {
	local_build_env
	mkdir -p dist/image
	case "${1:-}" in
	--all)
		section "image: build linux/amd64 and linux/arm64"
		rm -rf dist/image/oci
		ko_build --tags=dev --push=false --platform=linux/amd64,linux/arm64 \
			--oci-layout-path=dist/image/oci >/dev/null
		log_info "wrote dist/image/oci"
		;;
	"")
		section "image: build linux/$(host_arch)"
		ko_build --tags=dev --push=false --platform="linux/$(host_arch)" \
			--tarball=dist/image/gskill.tar >/dev/null
		log_info "wrote dist/image/gskill.tar; load and run it with:"
		printf '  docker load -i dist/image/gskill.tar   (or podman load -i)\n' >&2
		printf '  docker run --rm gskill.test/gskill:dev version\n' >&2
		;;
	*) usage ;;
	esac
}

# What the CI image job runs: both platforms build, and the host platform,
# loaded into Docker, passes the smoke suite. The only ko.local build.
check() {
	[ $# -eq 0 ] || usage
	local_build_env
	if ! have docker || ! docker info >/dev/null 2>&1; then
		die "image check needs a running Docker daemon"
	fi
	local arch ref
	arch="$(host_arch)"

	section "image check: build linux/amd64 and linux/arm64"
	ko_build --tags=dev --push=false --platform=linux/amd64,linux/arm64 >/dev/null ||
		die "image check failed at: build linux/amd64 and linux/arm64"

	section "image check: load linux/${arch} into Docker"
	ref="$(KO_DOCKER_REPO=ko.local/gskill ko_build --tags=check --platform="linux/${arch}")" ||
		die "image check failed at: load linux/${arch} into Docker"

	section "image check: smoke ${ref}"
	"$self" smoke "$ref" --version dev || die "image check failed at: smoke"
}

release_tag_re='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
stable_tag_re='^v[0-9]+\.[0-9]+\.[0-9]+$'
prerelease_tag_re='^v[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z.-]+$'

# Exit 2: a usage, registry, or auth error, as opposed to 1 for an absent tag.
die2() {
	log_error "$*"
	exit 2
}

# One scratch directory per run, removed on exit. Call it as a statement, not
# inside $(...), so the directory outlives the call.
scratch_dir() {
	if [ -z "${work:-}" ]; then
		work="$(mktemp -d)"
		trap 'rm -rf "$work"' EXIT
	fi
}

# Set highest to the highest tag on origin of the same kind as $1, stable or
# prerelease, or to $1 itself when there is none yet.
highest_on_origin() {
	local refs kind_re="$stable_tag_re"
	[[ "$1" != *-* ]] || kind_re="$prerelease_tag_re"
	refs="$(git ls-remote --tags --refs origin 'v*')" || die2 "could not list the tags on origin"
	highest="$(awk '{ sub("refs/tags/", "", $2); print $2 }' <<<"$refs" | grep -E "$kind_re" | sort -V | tail -n1 || true)"
	highest="${highest:-$1}"
}

# The tag policy: the bare version always, plus the channel of its kind
# (latest for a stable release, next for a prerelease) when it is at or above
# the highest tag of that kind. A backport, or a re-run for an older release
# candidate, never moves a channel back.
tags() {
	[ $# -eq 1 ] || [ $# -eq 2 ] || usage
	local tag="$1" kind=stable kind_re="$stable_tag_re" channel=latest highest
	[[ "$tag" =~ $release_tag_re ]] || die2 "invalid release tag '${tag}': want vX.Y.Z or vX.Y.Z-<prerelease>"
	if [[ "$tag" == *-* ]]; then
		kind=prerelease kind_re="$prerelease_tag_re" channel=next
	fi
	if [ $# -eq 2 ]; then
		highest="$2"
	else
		highest_on_origin "$tag"
	fi
	[[ "$highest" =~ $kind_re ]] || die2 "'${highest}' is not a ${kind} tag like '${tag}'"
	if [ "$(printf '%s\n' "$highest" "$tag" | sort -V | tail -n1)" = "$tag" ]; then
		echo "${tag#v},${channel}"
	else
		echo "${tag#v}"
	fi
}

# Split REF (host/name:tag, host/name@digest, or both) into reg_host,
# reg_name, and reg_ref, the digest when there is one. Like ko and Docker,
# talk plain HTTP to a registry on localhost.
parse_ref() {
	local repo="${1%%@*}" last
	reg_ref=""
	[[ "$1" != *@* ]] || reg_ref="${1#*@}"
	last="${repo##*/}"
	if [[ "$last" == *:* ]]; then
		[ -n "$reg_ref" ] || reg_ref="${last#*:}"
		repo="${repo%:*}"
	fi
	[ -n "$reg_ref" ] && [[ "${repo%%/*}" == *[.:]* || "${repo%%/*}" == localhost ]] ||
		die2 "want host/name:tag or host/name@sha256:..., got '${1}'"
	reg_host="${repo%%/*}"
	reg_name="${repo#*/}"
	case "$reg_host" in
	localhost:* | 127.0.0.1:*) reg_scheme=http ;;
	*) reg_scheme=https ;;
	esac
}

manifest_types="application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

# Print the HTTP status of a manifest request; the body and headers land in
# the scratch directory. curl reports 000 when it cannot connect.
manifest_request() {
	local args=(-sS -o "${work}/body" -D "${work}/headers" -w '%{http_code}' -H "Accept: ${manifest_types}")
	[ "$1" = GET ] || args+=(--head)
	[ -z "${reg_token:-}" ] || args+=(-H "Authorization: Bearer ${reg_token}")
	curl "${args[@]}" "$2" || true
}

# Trade the 401 challenge for a pull token: anonymous, or as GITHUB_ACTOR with
# GHCR_TOKEN, which is only ever sent to ghcr.io. The credentials go through
# stdin so they never appear in the process list.
registry_login() {
	local challenge realm service scope creds=""
	challenge="$(grep -i '^www-authenticate:' "${work}/headers" | tr -d '\r' || true)"
	realm="$(sed -n 's/.*realm="\([^"]*\)".*/\1/p' <<<"$challenge")"
	service="$(sed -n 's/.*service="\([^"]*\)".*/\1/p' <<<"$challenge")"
	scope="$(sed -n 's/.*scope="\([^"]*\)".*/\1/p' <<<"$challenge")"
	[ -n "$realm" ] || die2 "${reg_host}: HTTP 401 without a bearer challenge"
	if [ -n "${GHCR_TOKEN:-}" ] && [[ "$realm" == https://ghcr.io/* ]]; then
		creds="user = \"${GITHUB_ACTOR:?GHCR_TOKEN needs GITHUB_ACTOR}:${GHCR_TOKEN}\""
	fi
	reg_token="$(printf '%s\n' "$creds" | curl -sS --fail -K - -G "$realm" \
		--data-urlencode "service=${service}" \
		--data-urlencode "scope=${scope:-repository:${reg_name}:pull}" |
		jq -r '.token // .access_token // empty' || true)"
	[ -n "$reg_token" ] || die2 "${realm}: no pull token for ${reg_name}"
}

# Fetch a manifest of the parsed repository. Exits 1 when it does not exist
# and 2 on any other failure.
fetch_manifest() {
	local url="${reg_scheme}://${reg_host}/v2/${reg_name}/manifests/${2}" code
	code="$(manifest_request "$1" "$url")"
	if [ "$code" = 401 ] && [ -z "${reg_token:-}" ]; then
		registry_login
		code="$(manifest_request "$1" "$url")"
	fi
	case "$code" in
	200) ;;
	404) exit 1 ;;
	*) die2 "${1} ${url}: HTTP ${code}" ;;
	esac
}

registry_setup() {
	[ $# -eq 1 ] || usage
	have curl || die2 "missing curl"
	have jq || die2 "missing jq"
	parse_ref "$1"
	scratch_dir
}

digest() {
	registry_setup "$@"
	fetch_manifest HEAD "$reg_ref"
	local d
	d="$(grep -i '^docker-content-digest:' "${work}/headers" | tr -d '\r' | awk '{ print $2; exit }' || true)"
	# Exit 1 means absent to the publish guard, so a found manifest without a
	# digest must not look like one.
	[ -n "$d" ] || die2 "${1}: the registry sent no Docker-Content-Digest header"
	echo "$d"
}

# The size is what a pull downloads: the platform manifest's config and layers.
platforms() {
	registry_setup "$@"
	fetch_manifest GET "$reg_ref"
	jq -e '.manifests' "${work}/body" >/dev/null || die2 "${1} is not a multi-platform index"
	local entries platform digest lines=""
	entries="$(jq -r '.manifests[] | select(.platform != null and .platform.os != "unknown") | "\(.platform.os)/\(.platform.architecture) \(.digest)"' "${work}/body")"
	while read -r platform digest; do
		[ -n "$platform" ] || continue
		fetch_manifest GET "$digest"
		lines+="${platform} ${digest} $(jq '.config.size + ([.layers[].size] | add)' "${work}/body")"$'\n'
	done <<<"$entries"
	printf '%s' "$lines" | sort
}

ko_digest() {
	grep -o 'sha256:[0-9a-f]\{64\}' | tail -n1
}

# Release publication (image-release.yml). The version tag never moves: the
# image is built once, into an OCI layout, and its digest is compared with what
# the tag already points at before anything is pushed. crane then pushes that
# layout, the exact bytes the guard compared.
publish() {
	[ $# -eq 1 ] || usage
	[ "${GITHUB_ACTIONS:-}" = true ] || die "publish runs only in GitHub Actions (image-release.yml)"
	local tag="$1"
	[[ "$tag" =~ $release_tag_re ]] || die2 "invalid release tag '${tag}': want vX.Y.Z or vX.Y.Z-<prerelease>"
	need_tool ko
	need_tool crane
	local version="${tag#v}" layout=dist/image/release image_tags candidate current status=0 index plats amd64 arm64
	export GSKILL_VERSION="$version" KO_DOCKER_REPO="$IMAGE_REPO"

	section "image publish: build ${tag}"
	rm -rf "$layout"
	candidate="$(ko_build --push=false --platform=linux/amd64,linux/arm64 --oci-layout-path="$layout" | ko_digest)"
	[ -n "$candidate" ] || die "ko printed no digest"
	log_info "built ${candidate}"

	section "image publish: guard ${IMAGE_REPO}:${version}"
	current="$("$self" digest "${IMAGE_REPO}:${version}")" || status=$?
	case "$status" in
	0)
		[ "$current" = "$candidate" ] || die "${IMAGE_REPO}:${version} is ${current}, but this build is ${candidate}; refusing to move a version tag"
		log_info "${IMAGE_REPO}:${version} already points at the candidate"
		;;
	1) log_info "${IMAGE_REPO}:${version} does not exist yet" ;;
	*) die "could not read ${IMAGE_REPO}:${version} (see above)" ;;
	esac

	# Decided now, from the tags on origin, rather than at checkout: a run that
	# overlaps a newer release's run then cannot move a channel back.
	image_tags="$(tags "$tag")"

	section "image publish: push ${IMAGE_REPO} with tags ${image_tags}"
	index="$(crane push "$layout" "${IMAGE_REPO}:${version}" | ko_digest)"
	[ "$index" = "$candidate" ] || die "pushed ${index}, but the build was ${candidate}"
	if [[ "$image_tags" == *,* ]]; then
		crane tag "${IMAGE_REPO}@${index}" "${image_tags#*,}"
	fi

	plats="$("$self" platforms "${IMAGE_REPO}@${index}")"
	amd64="$(awk '$1 == "linux/amd64" { print $2 }' <<<"$plats")"
	arm64="$(awk '$1 == "linux/arm64" { print $2 }' <<<"$plats")"
	[ -n "$amd64" ] && [ -n "$arm64" ] || die "index ${index} lacks a platform: ${plats}"
	printf 'index=%s\namd64=%s\narm64=%s\nversion=%s\ntags=%s\n' \
		"$index" "$amd64" "$arm64" "$version" "$image_tags" | tee -a "${GITHUB_OUTPUT:-/dev/null}"
}

smoke_step() {
	smoke_n="$1" smoke_name="$2"
	section "smoke: ${smoke_n}. ${smoke_name}"
}

smoke_fail() {
	die "smoke failed at step ${smoke_n}: ${smoke_name}${1:+ (${1})}"
}

# Run the image as the caller, the way the docs do. Options before the gskill
# arguments: -w DIR mounts the workspace, -c DIR mounts the GSKILL home,
# -n cuts the network, -H leaves HOME at the image default.
run_gskill() {
	local args=(--rm --user "$(id -u):$(id -g)") home=yes
	while [ $# -gt 0 ]; do
		case "$1" in
		-w)
			args+=(-v "$2:/workspace" -w /workspace)
			shift 2
			;;
		-c)
			mkdir -p "$2"
			args+=(-e GSKILL_HOME=/cache -v "$2:/cache")
			shift 2
			;;
		-n)
			args+=(--network none)
			shift
			;;
		-H)
			home=no
			shift
			;;
		*) break ;;
		esac
	done
	[ "$home" = no ] || args+=(-e HOME=/tmp)
	docker run "${args[@]}" "$smoke_ref" "$@"
}

# A project set up the way scripts/check-landing-example.sh sets one up.
new_workspace() {
	mkdir -p "$1/.claude"
	git -C "$1" init --quiet -b main
	git -C "$1" config user.name "gskill image smoke"
	git -C "$1" config user.email "image-smoke@example.invalid"
}

# A fresh copy of a project without the installed skills, the same paths
# website/src/landing/example.sh deletes.
copy_without_skills() {
	cp -R "$1" "$2"
	rm -rf "$2/.agents/skills" "$2/.claude/skills"
}

same_skills() {
	diff -r "$1/.agents/skills" "$2/.agents/skills" >/dev/null &&
		diff -r "$1/.claude/skills" "$2/.claude/skills" >/dev/null
}

smoke() {
	local want="" skill="github.com/glapsfun/gskill/examples/skills/hello-gskill"
	smoke_ref="${1:-}"
	[ -n "$smoke_ref" ] || usage
	shift
	case "${1:-}" in
	"") ;;
	--version)
		[ -n "${2:-}" ] || usage
		want="$2"
		;;
	*) usage ;;
	esac
	have docker || die "missing docker: the smoke suite runs the image"
	have jq || die "missing jq"
	case "$(id -u)" in
	0 | 65532) die "run the smoke suite as a user other than root and the image default (65532)" ;;
	esac

	scratch_dir
	local w1="${work}/w1" w2="${work}/w2" w3="${work}/w3" w4="${work}/w4" c1="${work}/c1"
	local out="${work}/out" link

	smoke_step 1 "version"
	run_gskill version >"$out" || smoke_fail
	cat "$out" >&2
	[ -z "$want" ] || [ "$(cat "$out")" = "gskill ${want}" ] || smoke_fail "want gskill ${want}"

	smoke_step 2 "version --json names the commit"
	run_gskill version --json >"$out" || smoke_fail
	jq -e '.commit | type == "string" and length > 0' "$out" >/dev/null || smoke_fail "no commit in $(tr -d '\n' <"$out")"

	smoke_step 3 "--help"
	run_gskill --help >/dev/null || smoke_fail

	smoke_step 4 "add a skill as $(id -u):$(id -g), with a cache mount"
	new_workspace "$w1"
	run_gskill -w "$w1" -c "$c1" add "$skill" --agent claude || smoke_fail
	[ -f "${w1}/skills-lock.json" ] || smoke_fail "no skills-lock.json"
	[ -z "$(find "$w1" ! -user "$(id -u)" -print -quit)" ] || smoke_fail "files not owned by $(id -u)"
	link="${w1}/.claude/skills/hello-gskill"
	[ -f "${link}/SKILL.md" ] || smoke_fail ".claude/skills/hello-gskill does not resolve on the host"
	if [ -L "$link" ]; then
		case "$(readlink "$link")" in /*) smoke_fail "absolute link $(readlink "$link")" ;; esac
	fi
	git -C "$w1" add -A
	git -C "$w1" commit --quiet -m "Add hello-gskill"

	smoke_step 5 "install --frozen-lockfile in a fresh copy, workspace only"
	copy_without_skills "$w1" "$w2"
	run_gskill -w "$w2" install --frozen-lockfile || smoke_fail
	same_skills "$w1" "$w2" || smoke_fail "restored skills differ"

	smoke_step 6 "project verify"
	run_gskill -w "$w2" project verify || smoke_fail

	smoke_step 7 "without HOME=/tmp, add fails"
	new_workspace "$w4"
	if run_gskill -H -w "$w4" add "$skill" --agent claude 2>"$out"; then
		smoke_fail "add succeeded with no writable home"
	fi
	log_info "the expected error: $(cat "$out")"

	smoke_step 8 "offline install --frozen-lockfile from the warm cache mount"
	copy_without_skills "$w1" "$w3"
	run_gskill -w "$w3" -c "$c1" -n --offline install --frozen-lockfile || smoke_fail
	same_skills "$w1" "$w3" || smoke_fail "restored skills differ"

	section "smoke: PASSED (${smoke_ref})"
}

case "${1:-}" in
build)
	shift
	build "$@"
	;;
check)
	shift
	check "$@"
	;;
smoke)
	shift
	smoke "$@"
	;;
tags)
	shift
	tags "$@"
	;;
digest)
	shift
	digest "$@"
	;;
platforms)
	shift
	platforms "$@"
	;;
publish)
	shift
	publish "$@"
	;;
*)
	usage
	;;
esac
