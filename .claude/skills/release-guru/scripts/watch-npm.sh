#!/usr/bin/env bash
# Watch the npm Release run for a tag. release.yml dispatches npm-release.yml on
# the tag ref as its last step, so the run's headBranch is the tag. A red run is
# NOT proof the packages are missing (v0.7.2's run failed after publishing all
# five), so on failure this prints the registry state for the verdict.
# --redispatch retries: it dispatches npm-release.yml on the release branch
# (packaging code from the branch, binaries from the tag's release) and follows
# that new run, never an older one.
# Args: the tag (vX.Y.Z[-rc.N]), then optionally --redispatch.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need gh
tag="$(require_tag "${1:-}")"
ref="$tag"
after=0
case "${2:-}" in
"") ;;
--redispatch)
	check_identity
	ref="$RELEASE_BRANCH"
	after="$(newest_run_id "$NPM_WORKFLOW" "$ref")"
	gh workflow run "$NPM_WORKFLOW" --repo "$RELEASE_REPO" --ref "$ref" -f "tag=${tag}" ||
		die "could not dispatch ${NPM_WORKFLOW} on ${ref}"
	log_info "dispatched ${NPM_WORKFLOW} on ${ref} for ${tag}"
	;;
*) die "usage: $(basename "$0") <vX.Y.Z> [--redispatch]" ;;
esac

log_info "locating the npm Release run for ${tag}; it usually takes 1-3 minutes ..."
status=0
follow_run "$NPM_WORKFLOW" "$ref" "${after:-0}" NPM || status=$?
case "$status" in
0)
	log_info "npm Release run for ${tag} succeeded"
	exit 0
	;;
2) die "no npm Release run found for ${tag}; if the Release run's 'Trigger npm publish' step failed, dispatch it: $(basename "$0") ${tag} --redispatch" ;;
esac

log_error "the npm Release run for ${tag} failed; checking what actually reached the registry"
"$here/release-status.sh" "$tag" >&2 || true
die "npm run failed; use the registry state above and references/failure-playbook.md (npm section) to decide"
