#!/usr/bin/env bash
# Shared helpers for release-guru scripts. Sourced, not run directly.
set -euo pipefail

_color() { if [ -t 2 ]; then printf '%s' "$1"; fi; }
_reset() { _color $'\033[0m'; }

log_info()  { printf '%s[release-guru]%s %s\n' "$(_color $'\033[34m')" "$(_reset)" "$*" >&2; }
log_warn()  { printf '%s[release-guru]%s %s\n' "$(_color $'\033[33m')" "$(_reset)" "$*" >&2; }
log_error() { printf '%s[release-guru]%s %s\n' "$(_color $'\033[31m')" "$(_reset)" "$*" >&2; }
die()       { log_error "$*"; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }
need() { have "$1" || die "missing required tool '$1'"; }

# host_os_arch prints "<os>_<arch>" matching the GoReleaser archive name template,
# e.g. "darwin_arm64". It dies on an unsupported platform.
host_os_arch() {
	local os arch
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	case "$os" in
	linux) os=linux ;;
	darwin) os=darwin ;;
	*) die "unsupported OS '$os': gskill releases target linux and darwin only" ;;
	esac
	arch=$(uname -m)
	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "unsupported architecture '$arch': gskill releases target amd64 and arm64 only" ;;
	esac
	printf '%s_%s' "$os" "$arch"
}

# require_tag validates a vX.Y.Z or vX.Y.Z-rc.N argument and echoes it back.
require_tag() {
	local tag="${1:-}"
	[ -n "$tag" ] || die "usage: $(basename "$0") <vX.Y.Z>"
	printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' ||
		die "tag '$tag' must look like vX.Y.Z (optionally -rc.N)"
	printf '%s' "$tag"
}

# is_prerelease succeeds for a tag with a -suffix (e.g. v0.8.0-rc.1). Prereleases
# go to npm's `next` dist-tag and never touch Homebrew or install.sh's default.
is_prerelease() { case "$1" in *-*) return 0 ;; *) return 1 ;; esac; }

# check_identity fails unless the active gh account matches the git user, so a
# tag is never pushed or a workflow dispatched as the wrong person.
check_identity() {
	local git_user gh_user
	git_user="$(git config user.name || true)"
	[ -n "$git_user" ] || die "git user.name is not set"
	gh_user="$(gh api user --jq .login 2>/dev/null || true)"
	[ -n "$gh_user" ] || die "gh is not authenticated; run 'gh auth login'"
	[ "$git_user" = "$gh_user" ] ||
		die "gh account '$gh_user' does not match git user '$git_user'; run 'gh auth switch --user $git_user' and re-check with 'gh auth status'"
	log_info "identity OK: git user and gh account are both '$gh_user'"
}

# newest_run_id WORKFLOW REF prints the id of the newest run of WORKFLOW on REF
# (a tag or branch), or nothing. Run ids only grow, so a bigger id is newer.
newest_run_id() {
	gh run list --repo "$RELEASE_REPO" --workflow "$1" --limit 30 \
		--json databaseId,headBranch \
		--jq "[.[] | select(.headBranch == \"$2\")] | max_by(.databaseId) | .databaseId // empty" 2>/dev/null || true
}

# follow_run WORKFLOW REF AFTER NAME waits up to 3 minutes for a run of WORKFLOW
# on REF with an id above AFTER (0 accepts any), so a re-dispatch never picks up
# the run it replaces. It watches that run and prints NAME_RUN_URL=<url>, and
# returns 0 when the run succeeds, 1 when it fails (after printing the failed
# log), and 2 when no run appears.
follow_run() {
	local workflow="$1" ref="$2" after="$3" name="$4" id="" url
	for _ in $(seq 1 30); do
		id="$(newest_run_id "$workflow" "$ref")"
		if [ -n "$id" ] && [ "$id" -gt "$after" ]; then
			break
		fi
		id=""
		sleep 6
	done
	[ -n "$id" ] || return 2
	url="$(gh run view "$id" --repo "$RELEASE_REPO" --json url --jq .url 2>/dev/null || true)"
	log_info "watching ${workflow} run ${id}${url:+ ($url)}"
	if gh run watch "$id" --repo "$RELEASE_REPO" --exit-status --interval 15 >/dev/null; then
		echo "${name}_RUN_URL=${url}"
		return 0
	fi
	gh run view "$id" --repo "$RELEASE_REPO" --log-failed 2>/dev/null | tail -40 >&2 || true
	echo "${name}_RUN_URL=${url}"
	return 1
}

# Defaults, overridable from the environment (see SKILL.md).
RELEASE_REPO="${RELEASE_REPO:-glapsfun/gskill}"
RELEASE_WORKFLOW="${RELEASE_WORKFLOW:-release.yml}"
NPM_WORKFLOW="${NPM_WORKFLOW:-npm-release.yml}"
CI_WORKFLOW="${CI_WORKFLOW:-ci.yml}"
RELEASE_BINARY="${RELEASE_BINARY:-gskill}"
RELEASE_BRANCH="${RELEASE_BRANCH:-main}"
TAP_REPO="${TAP_REPO:-glapsfun/homebrew-tap}"
TAP_CASK_PATH="${TAP_CASK_PATH:-Casks/gskill.rb}"
NPM_LAUNCHER="${NPM_LAUNCHER:-@glapsfun/gskill}"
NPM_PACKAGES="${NPM_PACKAGES:-@glapsfun/gskill @glapsfun/gskill-darwin-x64 @glapsfun/gskill-darwin-arm64 @glapsfun/gskill-linux-x64 @glapsfun/gskill-linux-arm64}"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/glapsfun/gskill}"
IMAGE_WORKFLOW="${IMAGE_WORKFLOW:-image-release.yml}"
export RELEASE_REPO RELEASE_WORKFLOW NPM_WORKFLOW CI_WORKFLOW RELEASE_BINARY RELEASE_BRANCH \
	TAP_REPO TAP_CASK_PATH NPM_LAUNCHER NPM_PACKAGES IMAGE_REPO IMAGE_WORKFLOW
