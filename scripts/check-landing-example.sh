#!/usr/bin/env bash
# Run the landing page's workflow example (website/src/landing/example.sh) line
# by line in a fresh Git project, and fail on the first line that errors. The
# file is run as written in both modes:
#
#   check-landing-example.sh --local    gskill built from this checkout; git
#                                       fetches the example skill from a snapshot
#                                       of this checkout's examples/ (offline).
#                                       Blocking check on every pull request.
#   check-landing-example.sh --release  the release in $VERSION (default: latest
#                                       stable) via scripts/install.sh; example
#                                       skill from github.com. Run after each
#                                       publish and release; never blocks.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

mode="${1:-}"
case "$mode" in
--local | --release) ;;
*) die "usage: $(basename "$0") --local | --release" ;;
esac

example="${repo_root}/website/src/landing/example.sh"
[ -f "$example" ] || die "missing ${example}"
remote="https://github.com/glapsfun/gskill.git"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "${work}/bin" "${work}/home" "${work}/project/.claude"

case "$mode" in
--local)
	section "landing example: build gskill from the checkout"
	(cd "$repo_root" && go build -o "${work}/bin/gskill" ./cmd/gskill)
	;;
--release)
	section "landing example: install ${VERSION:-the latest release}"
	INSTALL_DIR="${work}/bin" sh "${repo_root}/scripts/install.sh"
	;;
esac

# A clean HOME and XDG dirs: no cached clones, no global store, no user config.
# Set after the build, which needs the real Go caches.
export HOME="${work}/home"
export XDG_CACHE_HOME="${work}/home/.cache" XDG_CONFIG_HOME="${work}/home/.config" XDG_DATA_HOME="${work}/home/.local/share"
export PATH="${work}/bin:${PATH}"

if [ "$mode" = --local ]; then
	# Serve the example skill from a one-commit snapshot of the working tree,
	# so uncommitted edits and CI's shallow, detached checkout both work.
	snapshot="${work}/remote"
	mkdir -p "$snapshot"
	cp -R "${repo_root}/examples" "${snapshot}/examples"
	git -C "$snapshot" init --quiet -b main
	git -C "$snapshot" add -A
	git -C "$snapshot" -c user.name=docs-check -c user.email=docs-check@example.invalid \
		commit --quiet -m "examples snapshot"

	# gskill passes GIT_CONFIG_* through to git, so this redirects the remote
	# source in example.sh to the snapshot without editing the example. The
	# entry is appended, so config already passed this way still applies.
	i="${GIT_CONFIG_COUNT:-0}"
	export GIT_CONFIG_COUNT=$((i + 1))
	export "GIT_CONFIG_KEY_${i}=url.${snapshot}.insteadOf"
	export "GIT_CONFIG_VALUE_${i}=${remote}"
fi
log_info "using $(command -v gskill): $(gskill version)"

cd "${work}/project"
git init --quiet -b main
git config user.name "gskill docs check"
git config user.email "docs-check@example.invalid"

section "landing example: run ${example#"${repo_root}/"} (${mode#--})"
n=0
while IFS= read -r line || [ -n "$line" ]; do
	n=$((n + 1))
	case "$line" in '' | '#'*) continue ;; esac
	log_info "\$ ${line}"
	if ! bash -euo pipefail -c "$line"; then
		die "example.sh line ${n} failed: ${line}"
	fi
done <"$example"

section "landing example: PASSED (${mode#--})"
