#!/usr/bin/env bash
# Watch the Release workflow run triggered by the tag until it finishes. Exits
# non-zero if the run fails, after printing which step failed and its log.
# Arg: the tag (vX.Y.Z[-rc.N]).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need gh
tag="$(require_tag "${1:-}")"

# The tag push creates a run whose headBranch is the tag name. Poll briefly for it
# to register before watching.
log_info "locating the Release run for ${tag} ..."
run_id=""
for _ in $(seq 1 30); do
	run_id="$(gh run list --repo "$RELEASE_REPO" --workflow "$RELEASE_WORKFLOW" --event push \
		--json databaseId,headBranch \
		--jq "[.[] | select(.headBranch==\"${tag}\")][0].databaseId" 2>/dev/null || true)"
	[ -n "$run_id" ] && [ "$run_id" != "null" ] && break
	sleep 4
done
[ -n "$run_id" ] && [ "$run_id" != "null" ] ||
	die "no Release run found for ${tag} yet; check: gh run list --workflow ${RELEASE_WORKFLOW}"

url="$(gh run view "$run_id" --repo "$RELEASE_REPO" --json url --jq .url 2>/dev/null || true)"
log_info "watching Release run ${run_id}${url:+ ($url)}, usually 8-12 minutes"

if ! gh run watch "$run_id" --repo "$RELEASE_REPO" --exit-status --interval 30 >/dev/null; then
	failed="$(gh run view "$run_id" --repo "$RELEASE_REPO" --json jobs \
		--jq '[.jobs[].steps[] | select(.conclusion=="failure") | .name] | join(", ")' 2>/dev/null || true)"
	log_error "the Release run failed at step: ${failed:-unknown}"
	gh run view "$run_id" --repo "$RELEASE_REPO" --log-failed 2>/dev/null | tail -60 >&2 || true
	echo "FAILED_STEP=${failed}"
	echo "RUN_URL=${url}"
	die "release run for ${tag} failed; classify it with references/failure-playbook.md before doing anything else"
fi
log_info "Release run for ${tag} succeeded"
echo "RUN_URL=${url}"
