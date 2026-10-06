#!/usr/bin/env bash
# Preflight checks before cutting a release. Fails closed on anything that would
# make the release unsafe. Arg: the target tag (vX.Y.Z[-rc.N]). Read-only:
# it fetches, but never tags, pushes, or dispatches anything.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need git
need gh
tag="$(require_tag "${1:-}")"

# Right person: the tag push and every gh call act as this account.
check_identity

# Verification tooling. Missing tools don't block the release, but they make the
# post-release verification report "skipped" for that check, so say so now.
have cosign || log_warn "cosign is not installed: the release signature will NOT be verified (install cosign for a full check)"
gh attestation verify --help >/dev/null 2>&1 ||
	log_warn "gh $(gh --version | head -1 | awk '{print $3}') has no 'gh attestation': provenance will NOT be verified (upgrade gh to >= 2.49)"
have npm || log_warn "npm is not installed: the npm channel cannot be verified"

# On the release branch.
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$RELEASE_BRANCH" ] ||
	die "on branch '$branch', but releases must come from '$RELEASE_BRANCH' (switch to it, or override with RELEASE_BRANCH)"

# Clean working tree; the release job refuses a dirty tree too.
[ -z "$(git status --porcelain)" ] ||
	die "working tree is dirty; commit or stash before releasing"

# In sync with upstream so the tagged commit is the one reviewers saw.
git fetch --quiet --tags origin || die "git fetch failed"
upstream="$(git rev-parse --abbrev-ref '@{u}' 2>/dev/null || true)"
[ -n "$upstream" ] || die "branch '$branch' has no upstream; push it first"
[ "$(git rev-parse @)" = "$(git rev-parse '@{u}')" ] ||
	die "branch '$branch' is not in sync with '$upstream'; pull/push until it matches"
sha="$(git rev-parse @)"

# The tag and release must not already exist (the workflow refuses to overwrite).
git rev-parse --verify --quiet "refs/tags/$tag" >/dev/null && die "tag '$tag' already exists locally"
if git ls-remote --tags origin "refs/tags/$tag" | grep -q .; then
	die "tag '$tag' already exists on origin"
fi
if gh release view "$tag" --repo "$RELEASE_REPO" >/dev/null 2>&1; then
	die "release '$tag' already exists on GitHub"
fi

# No other release in flight: release.yml serializes on one concurrency group,
# and two releases racing for npm `latest` is a mess to untangle.
inflight="$(gh run list --repo "$RELEASE_REPO" --workflow "$RELEASE_WORKFLOW" --limit 5 \
	--json status --jq '[.[] | select(.status != "completed")] | length' 2>/dev/null || echo 0)"
[ "$inflight" = "0" ] || die "a Release run is still in progress; wait for it before cutting another"

# CI is green for this exact commit. ci.yml includes release-dryrun, a GoReleaser
# snapshot build of exactly what would ship, so this is the cheapest proof that
# the tag will build.
ci="$(gh run list --repo "$RELEASE_REPO" --workflow "$CI_WORKFLOW" --branch "$RELEASE_BRANCH" \
	--commit "$sha" --event push --limit 1 --json status,conclusion,url \
	--jq '.[0] | "\(.status) \(.conclusion) \(.url)"' 2>/dev/null || true)"
case "$ci" in
"completed success "*) log_info "CI is green for ${sha:0:7} (${ci##* })" ;;
"") die "no CI run found for ${sha:0:7} on ${RELEASE_BRANCH}; wait for CI to run on this commit" ;;
completed*) die "CI for ${sha:0:7} did not succeed (${ci}); fix ${RELEASE_BRANCH} first" ;;
*) die "CI for ${sha:0:7} is still running (${ci}); wait for it to finish" ;;
esac

# Run the project quality gate locally too: the release job runs it on the
# tagged commit, but by then the tag is already public.
repo_root="$(git rev-parse --show-toplevel)"
if [ "${RELEASE_GURU_SKIP_GATE:-0}" = "1" ]; then
	log_warn "RELEASE_GURU_SKIP_GATE=1: skipping the local quality gate"
elif [ -x "$repo_root/scripts/verify.sh" ]; then
	log_info "running the quality gate ($repo_root/scripts/verify.sh), which takes a few minutes ..."
	(cd "$repo_root" && ./scripts/verify.sh) ||
		die "quality gate failed; fix it before tagging (a red gate would fail the release run)"
else
	die "quality gate not found at $repo_root/scripts/verify.sh; refusing to release unverified (set RELEASE_GURU_SKIP_GATE=1 only if you already ran it)"
fi

log_info "preflight OK: '$tag' from '$branch' @ ${sha:0:7}"
echo "SHA=${sha}"
