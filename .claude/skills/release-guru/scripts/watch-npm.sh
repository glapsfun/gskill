#!/usr/bin/env bash
# Watch the npm Release run for a tag. release.yml dispatches npm-release.yml on
# the tag ref as its last step, so the run's headBranch is the tag. A red run is
# NOT proof the packages are missing (v0.7.2's run failed after publishing all
# five), so on failure this prints the registry state for the verdict.
# Arg: the tag (vX.Y.Z[-rc.N]).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need gh
tag="$(require_tag "${1:-}")"

log_info "locating the npm Release run for ${tag} ..."
run_id=""
for _ in $(seq 1 30); do
	run_id="$(gh run list --repo "$RELEASE_REPO" --workflow "$NPM_WORKFLOW" --limit 20 \
		--json databaseId,headBranch,createdAt \
		--jq "[.[] | select(.headBranch==\"${tag}\")] | sort_by(.createdAt) | last | .databaseId" 2>/dev/null || true)"
	[ -n "$run_id" ] && [ "$run_id" != "null" ] && break
	sleep 6
done
[ -n "$run_id" ] && [ "$run_id" != "null" ] ||
	die "no npm Release run found for ${tag}; if the Release run's 'Trigger npm publish' step failed, dispatch it: gh workflow run ${NPM_WORKFLOW} --ref ${RELEASE_BRANCH} -f tag=${tag}"

url="$(gh run view "$run_id" --repo "$RELEASE_REPO" --json url --jq .url 2>/dev/null || true)"
log_info "watching npm Release run ${run_id}${url:+ ($url)}, usually 1-3 minutes"

if gh run watch "$run_id" --repo "$RELEASE_REPO" --exit-status --interval 15 >/dev/null; then
	log_info "npm Release run for ${tag} succeeded"
	echo "NPM_RUN_URL=${url}"
	exit 0
fi

log_error "the npm Release run for ${tag} failed; checking what actually reached the registry"
gh run view "$run_id" --repo "$RELEASE_REPO" --log-failed 2>/dev/null | tail -40 >&2 || true
"$here/release-status.sh" "$tag" >&2 || true
echo "NPM_RUN_URL=${url}"
die "npm run failed; use the registry state above and references/failure-playbook.md (npm section) to decide"
