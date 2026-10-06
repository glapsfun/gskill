#!/usr/bin/env bash
# One-screen, read-only status of a release across every stage, for resuming an
# interrupted release or diagnosing a failed one. Never changes anything.
# Arg: the tag (vX.Y.Z[-rc.N]).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need gh
need git
tag="$(require_tag "${1:-}")"
ver="${tag#v}"

row() { printf '  %-16s %s\n' "$1" "$2"; }

# An annotated tag lists twice: the tag object, then "<tag>^{}" for the commit
# it points to. Show the commit.
remote_sha="$(git ls-remote --tags origin "refs/tags/${tag}" "refs/tags/${tag}^{}" 2>/dev/null |
	awk '/\^\{\}$/ { peeled = substr($1, 1, 7) } !/\^\{\}$/ { plain = substr($1, 1, 7) } END { print (peeled != "" ? peeled : plain) }')"

run_line() {
	gh run list --repo "$RELEASE_REPO" --workflow "$1" --limit 30 \
		--json headBranch,status,conclusion,url,createdAt \
		--jq "[.[] | select(.headBranch==\"${tag}\")] | sort_by(.createdAt) | last |
			if . == null then \"none\" else \"\(.status)/\(.conclusion // \"-\") \(.url)\" end" 2>/dev/null || echo "unknown"
}

rel="$(gh release view "$tag" --repo "$RELEASE_REPO" --json isPrerelease,isDraft,assets,publishedAt \
	--jq '"published \(.publishedAt) prerelease=\(.isPrerelease) assets=\(.assets | length)"' 2>/dev/null || echo "none")"

echo "release status for ${tag} (${RELEASE_REPO})"
row "tag on origin" "${remote_sha:-none}"
row "release run" "$(run_line "$RELEASE_WORKFLOW")"
row "GitHub release" "$rel"
row "npm run" "$(run_line "$NPM_WORKFLOW")"

if have npm; then
	published=0
	total=0
	for pkg in $NPM_PACKAGES; do
		total=$((total + 1))
		[ "$(npm view "${pkg}@${ver}" version 2>/dev/null || true)" = "$ver" ] && published=$((published + 1))
	done
	row "npm packages" "${published}/${total} published at ${ver}"
	row "npm dist-tags" "$(npm view "$NPM_LAUNCHER" dist-tags --json 2>/dev/null | tr -d ' \n' || echo unknown)"
else
	row "npm packages" "unknown (npm not installed)"
fi

cask_ver="$(gh api "repos/${TAP_REPO}/contents/${TAP_CASK_PATH}" --jq .content 2>/dev/null | base64 -d 2>/dev/null |
	sed -n 's/^[[:space:]]*version "\(.*\)"/\1/p' | head -1 || true)"
row "homebrew cask" "${cask_ver:-unknown}"
row "latest release" "$(gh api "repos/${RELEASE_REPO}/releases/latest" --jq .tag_name 2>/dev/null || echo unknown)"
