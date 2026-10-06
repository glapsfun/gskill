#!/usr/bin/env bash
# Suggest the next semver tag from conventional commits since the last tag.
# Prints a human-readable summary to stderr and machine-readable
# `LAST=`, `SUGGESTED=vX.Y.Z` and `BUMP=<level>` lines to stdout.
#
# gskill merges PRs with merge commits whose body is the PR title, and the PR
# title is the one string CI forces to be conventional (pr-title.yml); branch
# commits often are not. So the signal is: every merge commit's PR title plus
# every non-merge commit subject since the last tag.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$here/lib.sh"

need git
git fetch --quiet --tags origin 2>/dev/null || log_warn "git fetch failed; using local tags"

last="$(git describe --tags --abbrev=0 --match 'v[0-9]*' "origin/${RELEASE_BRANCH}" 2>/dev/null ||
	git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)"
if [ -z "$last" ]; then
	log_info "no prior version tag found; suggesting the first release"
	echo "LAST="
	echo "SUGGESTED=v0.1.0"
	echo "BUMP=initial"
	exit 0
fi

# Compare against the release branch as it is on origin: that is what gets tagged.
head_ref="origin/${RELEASE_BRANCH}"
git rev-parse --verify --quiet "$head_ref" >/dev/null || head_ref="HEAD"
range="${last}..${head_ref}"

# Keep only the first non-empty body line of each merge commit (the PR title).
pr_titles="$(git log --merges --pretty='%x1e%b' "$range" |
	awk 'BEGIN { RS = "\x1e" } { n = split($0, l, "\n"); for (i = 1; i <= n; i++) if (l[i] ~ /[^[:space:]]/) { print l[i]; break } }')"
subjects="$(git log --no-merges --pretty=%s "$range")"
bodies="$(git log --pretty=%B "$range")"

count="$(git rev-list --count "$range")"
if [ "$count" -eq 0 ]; then
	log_warn "no new commits on ${head_ref} since ${last}; there is nothing to release"
	echo "LAST=${last}"
	echo "SUGGESTED=${last}"
	echo "BUMP=none"
	exit 0
fi

signals="$(printf '%s\n%s\n' "$pr_titles" "$subjects")"
bump="patch"
if printf '%s' "$bodies" | grep -qE '^BREAKING[ -]CHANGE' ||
	printf '%s' "$signals" | grep -qE '^[a-z]+(\(.+\))?!:'; then
	bump="major"
elif printf '%s' "$signals" | grep -qE '^feat(\(.+\))?:'; then
	bump="minor"
fi

# Split the last tag's core "M.m.p" (drop the leading v and any -rc suffix).
core="${last#v}"
core="${core%%-*}"
IFS=. read -r major minor patch <<<"$core"

alt=""
case "$bump" in
major)
	if [ "$major" -eq 0 ]; then
		# Pre-1.0 convention: a breaking change bumps the minor. 1.0.0 is a
		# deliberate product decision, offered only as the alternative.
		next="v0.$((minor + 1)).0"
		alt="v1.0.0"
	else
		next="v$((major + 1)).0.0"
	fi
	;;
minor) next="v${major}.$((minor + 1)).0" ;;
patch) next="v${major}.${minor}.$((patch + 1))" ;;
esac

log_info "since ${last} on ${head_ref}: ${count} commit(s) → ${bump} bump → ${next}"
if [ -n "$pr_titles" ]; then
	log_info "merged PRs:"
	printf '%s\n' "$pr_titles" | sed 's/^/  • /' >&2
fi
log_info "commits:"
printf '%s\n' "$subjects" | sed 's/^/  • /' >&2
if [ -n "$alt" ]; then
	log_warn "breaking change in a 0.x project: suggesting ${next} (minor bump, the pre-1.0 convention); ${alt} is the alternative and needs an explicit decision from the user"
fi
if [ "$head_ref" != "HEAD" ] && [ "$(git rev-parse HEAD)" != "$(git rev-parse "$head_ref")" ]; then
	log_warn "local HEAD differs from ${head_ref}; the suggestion describes ${head_ref}, which is what a release would tag"
fi

echo "LAST=${last}"
echo "SUGGESTED=${next}"
echo "BUMP=${bump}"
[ -z "$alt" ] || echo "ALTERNATIVE=${alt}"
