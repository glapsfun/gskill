#!/usr/bin/env bash
# Watch the Image Release run for a tag. release.yml dispatches image-release.yml
# on the tag ref as its last step, so the run's headBranch is the tag. On
# failure this prints the failed log and the release status for the verdict.
# --redispatch retries: it dispatches image-release.yml on the tag ref and
# follows that new run, never the one it replaces.
# Args: the tag (vX.Y.Z[-rc.N]), then optionally --redispatch.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need gh
tag="$(require_tag "${1:-}")"
after=0
case "${2:-}" in
"") ;;
--redispatch)
	check_identity
	after="$(newest_run_id "$IMAGE_WORKFLOW" "$tag")"
	gh workflow run "$IMAGE_WORKFLOW" --repo "$RELEASE_REPO" --ref "$tag" -f "tag=${tag}" ||
		die "could not dispatch ${IMAGE_WORKFLOW} on ${tag}"
	log_info "dispatched ${IMAGE_WORKFLOW} on ${tag}"
	;;
*) die "usage: $(basename "$0") <vX.Y.Z> [--redispatch]" ;;
esac

log_info "locating the Image Release run for ${tag}; it takes about 10 minutes ..."
status=0
follow_run "$IMAGE_WORKFLOW" "$tag" "${after:-0}" IMAGE || status=$?
case "$status" in
0)
	log_info "Image Release run for ${tag} succeeded"
	exit 0
	;;
2) die "no Image Release run found for ${tag}; if the Release run's 'Trigger image publish' step failed, dispatch it: $(basename "$0") ${tag} --redispatch" ;;
esac

log_error "the Image Release run for ${tag} failed"
"$here/release-status.sh" "$tag" >&2 || true
die "image run failed; classify it with references/failure-playbook.md (image section) before re-dispatching with: $(basename "$0") ${tag} --redispatch"
