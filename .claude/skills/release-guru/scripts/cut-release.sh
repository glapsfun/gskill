#!/usr/bin/env bash
# Create and push the annotated release tag. Pushing the tag triggers the Release
# workflow, the point of no return. Args: the tag and the commit SHA that
# preflight.sh approved; the tag goes on exactly that commit or not at all.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need git
need gh
tag="$(require_tag "${1:-}")"
approved="${2:-}"
[ -n "$approved" ] || die "usage: $(basename "$0") <vX.Y.Z> <sha-from-preflight>"

# Re-check what may have changed while the user was confirming.
check_identity
git fetch --quiet --tags origin || die "git fetch failed"
sha="$(git rev-parse "origin/${RELEASE_BRANCH}")"
[ "$sha" = "$(git rev-parse "$approved")" ] ||
	die "origin/${RELEASE_BRANCH} moved since preflight (${approved:0:7} → ${sha:0:7}); re-run preflight and re-confirm"
if git ls-remote --tags origin "refs/tags/$tag" | grep -q .; then
	die "tag '$tag' appeared on origin since preflight; stop and check who is releasing"
fi

git tag -a "$tag" "$sha" -m "$tag"
log_info "created annotated tag ${tag} on ${sha:0:7}"

if ! git push origin "refs/tags/$tag"; then
	# Roll back the local tag so a retry starts clean. Nothing public happened.
	git tag -d "$tag" >/dev/null 2>&1 || true
	die "failed to push ${tag}; local tag removed so you can retry"
fi
log_info "pushed ${tag}; the Release workflow starts now"
